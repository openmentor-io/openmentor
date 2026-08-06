package services

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Deletion telemetry vocabulary (D70), shared by the mentor's own deletion and
// the admin one so a trace filter written against one matches the other.
const (
	deletionActionDelete  = "delete"
	deletionActionRestore = "restore"

	deletionInitiatorMentor = "mentor"
	deletionInitiatorAdmin  = "admin"
)

// annotateDeletionSpan records the outcome of a deletion or restore on the
// request's server span (otelgin's), so a trace answers "what happened to this
// profile and why" without joining to logs.
//
// It annotates the EXISTING span rather than starting a child: there is exactly
// one span per request already, the operation is not a distinct unit of work
// inside it, and a child span would just be a duplicate with a different name.
//
// A non-success outcome sets the span status to Error even when the HTTP
// response is a 4xx. That is deliberate and differs from the transport view: a
// rejected deletion confirmation is a normal 400 to the load balancer, but when
// you are looking at deletion traces it is the thing you came to find.
//
// NO PII: the mentor UUID identifies the row, and it is already what the
// analytics events and logs carry. The typed confirmation and the mentor's
// email deliberately never reach a span — spans are sampled into a third-party
// backend and P14 exists because capabilities and identifiers leaked there
// before.
func annotateDeletionSpan(ctx context.Context, action, initiator, mentorID, outcome string) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.SetAttributes(
		attribute.String("mentor.id", mentorID),
		attribute.String("deletion.action", action),
		attribute.String("deletion.initiator", initiator),
		attribute.String("deletion.outcome", outcome),
	)
	if outcome != outcomeSuccess {
		span.SetStatus(codes.Error, outcome)
	}
}

// outcomeSuccess is the single spelling of a successful outcome across the
// deletion analytics properties and span attributes.
const outcomeSuccess = "success"
