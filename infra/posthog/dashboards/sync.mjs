#!/usr/bin/env node

import process from "node:process";
import { buildDashboardSpecs, DASHBOARD_SPEC_VERSION, MANAGED_TAG } from "./spec.mjs";

const args = new Set(process.argv.slice(2));
const validateOnly = args.has("--validate");
const dryRun = args.has("--dry-run") || asBool(process.env.POSTHOG_DRY_RUN);
const projectId = process.env.POSTHOG_PROJECT_ID;
const host = normalizeHost(process.env.POSTHOG_HOST || "https://app.posthog.com");
const personalApiKey = process.env.POSTHOG_PERSONAL_API_KEY;
const analyticsEnvironment = (process.env.POSTHOG_DASHBOARD_ENVIRONMENT || "production").trim() || "production";

class PostHogClient {
    constructor({ host, projectId, personalApiKey, dryRun }) {
        this.host = host;
        this.projectId = projectId;
        this.personalApiKey = personalApiKey;
        this.dryRun = dryRun;
        this.dryRunId = 1_000_000;
        this.projectBase = `${this.host}/api/projects/${this.projectId}`;
    }

    async list(path, params = {}) {
        let url = this.buildUrl(path, params);
        const results = [];

        while (url) {
            const payload = await this.request("GET", url);

            if (Array.isArray(payload?.results)) {
                results.push(...payload.results);
                url = payload.next || null;
                continue;
            }

            if (Array.isArray(payload)) {
                results.push(...payload);
            }
            break;
        }

        return results;
    }

    async create(path, body) {
        return this.request("POST", path, body);
    }

    async patch(path, body) {
        return this.request("PATCH", path, body);
    }

    buildUrl(path, params = {}) {
        const isAbsolute = typeof path === "string" && /^https?:\/\//i.test(path);
        const base = isAbsolute ? path : `${this.projectBase}${normalizePath(path)}`;
        const url = new URL(base);

        for (const [key, value] of Object.entries(params)) {
            if (value === undefined || value === null || value === "") {
                continue;
            }
            url.searchParams.set(key, String(value));
        }

        return url.toString();
    }

    async request(method, pathOrUrl, body) {
        const isAbsolute = typeof pathOrUrl === "string" && /^https?:\/\//i.test(pathOrUrl);
        const url = isAbsolute ? pathOrUrl : this.buildUrl(pathOrUrl);

        if (this.dryRun && method !== "GET") {
            const fakeId = extractIdFromUrl(url) || this.dryRunId++;
            console.log(`[posthog][dry-run] ${method} ${url}`);
            return { id: fakeId, ...(body || {}) };
        }

        const response = await fetch(url, {
            method,
            headers: {
                Accept: "application/json",
                Authorization: `Bearer ${this.personalApiKey}`,
                "Content-Type": "application/json",
            },
            body: body ? JSON.stringify(body) : undefined,
        });

        if (response.status === 204) {
            return {};
        }

        const text = await response.text();
        let payload = null;
        if (text) {
            try {
                payload = JSON.parse(text);
            } catch {
                payload = { raw: text };
            }
        }

        if (!response.ok) {
            throw new Error(
                `PostHog API ${method} ${url} failed with ${response.status}: ${JSON.stringify(payload)}`,
            );
        }

        return payload;
    }
}

const specs = buildDashboardSpecs({ environment: analyticsEnvironment });
validateSpecs(specs);

if (validateOnly) {
    printSpecSummary(specs);
    process.exit(0);
}

if (!projectId || !projectId.trim()) {
    throw new Error("POSTHOG_PROJECT_ID is required");
}
if (!personalApiKey || !personalApiKey.trim()) {
    throw new Error("POSTHOG_PERSONAL_API_KEY is required");
}

const client = new PostHogClient({
    host,
    projectId: projectId.trim(),
    personalApiKey: personalApiKey.trim(),
    dryRun,
});

console.log(
    `[posthog] syncing dashboard spec version ${DASHBOARD_SPEC_VERSION}` +
        ` (environment=${analyticsEnvironment}, dry_run=${dryRun})`,
);

const syncStats = await syncDashboards(client, specs);
printSyncSummary(syncStats);

async function syncDashboards(client, specs) {
    const stats = {
        dashboardsCreated: 0,
        dashboardsUpdated: 0,
        insightsCreated: 0,
        insightsUpdated: 0,
        dashboards: [],
    };

    const existingDashboards = await client.list("/dashboards/", { limit: 100, offset: 0 });
    const existingInsights = await client.list("/insights/", { limit: 200, offset: 0 });

    const dashboardByManagedTag = new Map();
    const dashboardByName = new Map();
    for (const dashboard of existingDashboards) {
        dashboardByName.set(dashboard.name, dashboard);
        for (const tag of dashboard.tags || []) {
            if (tag.startsWith("managed:openmentor:dashboard:")) {
                dashboardByManagedTag.set(tag, dashboard);
            }
        }
    }

    const insightByManagedTag = new Map();
    for (const insight of existingInsights) {
        for (const tag of insight.tags || []) {
            if (tag.startsWith("managed:openmentor:insight:")) {
                insightByManagedTag.set(tag, insight);
            }
        }
    }

    for (const dashboardSpec of specs) {
        const dashboardTag = dashboardManagedTag(dashboardSpec.key);
        const existingDashboard =
            dashboardByManagedTag.get(dashboardTag) || dashboardByName.get(dashboardSpec.name);

        const dashboardPayload = {
            description: dashboardSpec.description,
            name: dashboardSpec.name,
            pinned: false,
            tags: mergeUniqueTags(existingDashboard?.tags, dashboardSpec.tags, [
                MANAGED_TAG,
                dashboardTag,
            ]),
        };

        const dashboard = existingDashboard
            ? await client.patch(`/dashboards/${existingDashboard.id}/`, dashboardPayload)
            : await client.create("/dashboards/", dashboardPayload);

        if (existingDashboard) {
            stats.dashboardsUpdated += 1;
        } else {
            stats.dashboardsCreated += 1;
        }

        dashboardByManagedTag.set(dashboardTag, dashboard);
        dashboardByName.set(dashboard.name, dashboard);

        const dashboardResult = {
            dashboardId: dashboard.id,
            dashboardKey: dashboardSpec.key,
            dashboardName: dashboardSpec.name,
            insightsCreated: 0,
            insightsUpdated: 0,
        };

        for (const [index, insightSpec] of dashboardSpec.insights.entries()) {
            const insightTag = insightManagedTag(insightSpec.key);
            const existingInsight = insightByManagedTag.get(insightTag);
            const dashboards = mergeUniqueNumbers(existingInsight?.dashboards, [dashboard.id]);

            const insightPayload = {
                dashboards,
                description: insightSpec.description,
                name: insightSpec.name,
                order: index,
                query: insightSpec.query,
                tags: mergeUniqueTags(existingInsight?.tags, insightSpec.tags, [
                    MANAGED_TAG,
                    dashboardTag,
                    insightTag,
                ]),
            };

            const insight = existingInsight
                ? await client.patch(`/insights/${existingInsight.id}/`, insightPayload)
                : await client.create("/insights/", insightPayload);

            if (existingInsight) {
                stats.insightsUpdated += 1;
                dashboardResult.insightsUpdated += 1;
            } else {
                stats.insightsCreated += 1;
                dashboardResult.insightsCreated += 1;
            }

            insightByManagedTag.set(insightTag, insight);
        }

        stats.dashboards.push(dashboardResult);
    }

    return stats;
}

function mergeUniqueTags(...tagGroups) {
    const values = new Set();
    for (const group of tagGroups) {
        for (const tag of group || []) {
            if (typeof tag === "string" && tag.trim()) {
                values.add(tag.trim());
            }
        }
    }
    return [...values];
}

function mergeUniqueNumbers(...groups) {
    const values = new Set();
    for (const group of groups) {
        for (const value of group || []) {
            if (typeof value === "number" && Number.isFinite(value)) {
                values.add(value);
            }
        }
    }
    return [...values];
}

function validateSpecs(specs) {
    if (!Array.isArray(specs) || specs.length === 0) {
        throw new Error("Dashboard spec list is empty");
    }

    const dashboardKeys = new Set();
    const dashboardNames = new Set();
    const insightKeys = new Set();

    for (const dashboardSpec of specs) {
        if (!dashboardSpec.key || !dashboardSpec.name) {
            throw new Error("Each dashboard must have key and name");
        }
        if (dashboardKeys.has(dashboardSpec.key)) {
            throw new Error(`Duplicate dashboard key: ${dashboardSpec.key}`);
        }
        if (dashboardNames.has(dashboardSpec.name)) {
            throw new Error(`Duplicate dashboard name: ${dashboardSpec.name}`);
        }
        dashboardKeys.add(dashboardSpec.key);
        dashboardNames.add(dashboardSpec.name);

        if (!Array.isArray(dashboardSpec.insights) || dashboardSpec.insights.length === 0) {
            throw new Error(`Dashboard ${dashboardSpec.key} has no insights`);
        }

        for (const insightSpec of dashboardSpec.insights) {
            if (!insightSpec.key || !insightSpec.name) {
                throw new Error(`Dashboard ${dashboardSpec.key} has insight without key/name`);
            }
            if (insightKeys.has(insightSpec.key)) {
                throw new Error(`Duplicate insight key: ${insightSpec.key}`);
            }
            insightKeys.add(insightSpec.key);

            if (!insightSpec.query || insightSpec.query.kind !== "InsightVizNode") {
                throw new Error(`Insight ${insightSpec.key} has invalid query.kind`);
            }
            if (!insightSpec.query.source || insightSpec.query.source.kind !== "TrendsQuery") {
                throw new Error(`Insight ${insightSpec.key} has invalid query.source.kind`);
            }
            if (!Array.isArray(insightSpec.query.source.series) || insightSpec.query.source.series.length === 0) {
                throw new Error(`Insight ${insightSpec.key} has no query series`);
            }
        }
    }
}

function printSpecSummary(specs) {
    const insightCount = specs.reduce((count, dashboardSpec) => count + dashboardSpec.insights.length, 0);
    console.log(`[posthog] spec version ${DASHBOARD_SPEC_VERSION}`);
    console.log(`[posthog] dashboards: ${specs.length}, insights: ${insightCount}`);
    for (const dashboardSpec of specs) {
        console.log(`- ${dashboardSpec.name}: ${dashboardSpec.insights.length} insights`);
    }
}

function printSyncSummary(stats) {
    console.log("");
    console.log("[posthog] sync summary");
    console.log(`- dashboards created: ${stats.dashboardsCreated}`);
    console.log(`- dashboards updated: ${stats.dashboardsUpdated}`);
    console.log(`- insights created: ${stats.insightsCreated}`);
    console.log(`- insights updated: ${stats.insightsUpdated}`);
    console.log("");
    for (const dashboard of stats.dashboards) {
        console.log(
            `- ${dashboard.dashboardName} (#${dashboard.dashboardId}): ` +
                `${dashboard.insightsCreated} created, ${dashboard.insightsUpdated} updated`,
        );
    }
}

function dashboardManagedTag(dashboardKey) {
    return `managed:openmentor:dashboard:${dashboardKey}`;
}

function insightManagedTag(insightKey) {
    return `managed:openmentor:insight:${insightKey}`;
}

function normalizePath(path) {
    if (!path || path === "/") {
        return "/";
    }

    if (!path.startsWith("/")) {
        return `/${path}`;
    }
    return path;
}

function normalizeHost(host) {
    return String(host || "https://app.posthog.com").replace(/\/+$/, "");
}

function asBool(value) {
    return String(value || "").trim().toLowerCase() === "true";
}

function extractIdFromUrl(url) {
    const match = String(url).match(/\/(\d+)\/?(\?.*)?$/);
    if (!match) {
        return null;
    }
    return Number.parseInt(match[1], 10);
}

