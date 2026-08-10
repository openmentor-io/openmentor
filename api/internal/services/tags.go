package services

import "context"

// tagResolver is the slice of the mentor repository needed to turn tag names
// into ids.
type tagResolver interface {
	GetTagIDByName(ctx context.Context, tagName string) (string, error)
}

// resolveTagsStrict maps tag names onto tag ids and separately reports the
// names that do not exist.
//
// Callers MUST reject the write when unresolved is non-empty. Mentor tag
// updates replace the whole set (replaceMentorTags deletes then re-inserts), so
// quietly skipping an unknown name silently deletes that association — and a
// PARTIAL mismatch is the likely shape of the problem, not a total one: the
// D30 taxonomy deliberately left several tags unchanged, so a stale client
// typically sends a mix of names that still resolve and names that no longer
// do (e.g. "Backend" + the pre-D30 "QA"). Dropping only the unknown half looks
// like a successful save while quietly shrinking the mentor's profile.
func resolveTagsStrict(ctx context.Context, repo tagResolver, tags []string) (ids []string, unresolved []string) {
	ids = make([]string, 0, len(tags))
	for _, name := range tags {
		id, err := repo.GetTagIDByName(ctx, name)
		if err == nil && id != "" {
			ids = append(ids, id)
			continue
		}
		unresolved = append(unresolved, name)
	}
	return ids, unresolved
}
