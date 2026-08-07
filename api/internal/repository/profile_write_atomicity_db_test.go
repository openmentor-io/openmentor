package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// C1's guarantee is that a profile save writes the mentor row and its tag set or
// neither. That is a property of ONE Postgres transaction — a rolled back row
// update, a WHERE that stops matching once a delete commits — so it cannot be
// modeled by a fake repository. These tests need a real database and skip
// without one (see api/test/dbtest).

// seedTag inserts a tag and returns its id.
func seedTag(t *testing.T, pool *pgxpool.Pool, name string) string {
	t.Helper()

	var id string
	require.NoError(t, pool.QueryRow(context.Background(), `
		INSERT INTO tags (name) VALUES ($1)
		ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, name).Scan(&id))
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tags WHERE id = $1`, id)
	})
	return id
}

// readTagNames returns a mentor's current tag names.
func readTagNames(t *testing.T, pool *pgxpool.Pool, mentorID string) []string {
	t.Helper()

	rows, err := pool.Query(context.Background(), `
		SELECT t.name FROM mentor_tags mt
		JOIN tags t ON t.id = mt.tag_id
		WHERE mt.mentor_id = $1
	`, mentorID)
	require.NoError(t, err)
	defer rows.Close()

	names := make([]string, 0)
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	return names
}

func readAbout(t *testing.T, pool *pgxpool.Pool, mentorID string) string {
	t.Helper()
	var about string
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT COALESCE(about, '') FROM mentors WHERE id = $1`, mentorID).Scan(&about))
	return about
}

// TestUpdateProfileWithTagsRollsBackTheRowWhenTagsFail is the C1 defect in test
// form. The row update and the tag replacement used to be two operations, and on
// the mentor's own save path a tags failure was LOGGED while success was still
// returned — so the profile text moved and the tag set did not, with nothing
// telling the mentor.
func TestUpdateProfileWithTagsRollsBackTheRowWhenTagsFail(t *testing.T) {
	repo, pool := updateTestRepo(t)
	ctx := context.Background()
	id := seedMentor(t, pool, "profile-atomic-rollback")
	tagID := seedTag(t, pool, "profile-atomic-rollback-tag")

	require.NoError(t, repo.UpdateProfileWithTags(ctx, id,
		map[string]interface{}{"about": "original bio"}, []string{tagID}))
	require.Equal(t, []string{"profile-atomic-rollback-tag"}, readTagNames(t, pool, id))

	// A tag id that does not exist: mentor_tags.tag_id is a foreign key, so the
	// INSERT fails after the row UPDATE has already been issued in the same
	// transaction.
	err := repo.UpdateProfileWithTags(ctx, id,
		map[string]interface{}{"about": "bio that must not survive"},
		[]string{"00000000-0000-0000-0000-0000000000fe"})
	require.Error(t, err)

	assert.Equal(t, "original bio", readAbout(t, pool, id),
		"a tags failure must take the row update with it")
	assert.Equal(t, []string{"profile-atomic-rollback-tag"}, readTagNames(t, pool, id),
		"the previous tag set must survive a failed replacement")
}

// TestUpdateProfileWithTagsRefusesADeletedProfile: deletion (D70) makes a profile
// inert. The row UPDATE carries `deleted_at IS NULL`, so it matches nothing — and
// the point of checking the affected count is that the tag replacement, which has
// no such predicate, must not run anyway.
func TestUpdateProfileWithTagsRefusesADeletedProfile(t *testing.T) {
	repo, pool := updateTestRepo(t)
	ctx := context.Background()
	id := seedMentor(t, pool, "profile-atomic-deleted")
	tagID := seedTag(t, pool, "profile-atomic-deleted-tag")

	require.NoError(t, repo.UpdateProfileWithTags(ctx, id,
		map[string]interface{}{"about": "before deletion"}, []string{tagID}))

	_, err := repo.SoftDeleteMentor(ctx, id)
	require.NoError(t, err)

	err = repo.UpdateProfileWithTags(ctx, id,
		map[string]interface{}{"about": "after deletion"}, nil)
	require.ErrorIs(t, err, ErrMentorNotWritable)

	assert.Equal(t, "before deletion", readAbout(t, pool, id))
	assert.Equal(t, []string{"profile-atomic-deleted-tag"}, readTagNames(t, pool, id),
		"a refused save must not have emptied the tag set on the way out")
}

// TestUpdateProfileWithTagsStaysConsistentUnderConcurrentSaves runs the writers
// against each other, because a sequential test also passes against the
// non-transactional version: what a transaction buys is that no OTHER writer, and
// no deletion, can observe or leave a row whose text belongs to one save and
// whose tags belong to another.
//
// Each writer stamps its own index into `about` and claims exactly one tag named
// after the same index, so the final state is self-describing: about "save-3" with
// any tag set other than {tag-3} is a torn write.
func TestUpdateProfileWithTagsStaysConsistentUnderConcurrentSaves(t *testing.T) {
	repo, pool := updateTestRepo(t)
	ctx := context.Background()
	id := seedMentor(t, pool, "profile-atomic-concurrent")

	const writers = 8
	tagIDs := make([]string, writers)
	for i := range tagIDs {
		tagIDs[i] = seedTag(t, pool, fmt.Sprintf("profile-atomic-concurrent-%d", i))
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	errs := make([]error, writers)

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = repo.UpdateProfileWithTags(ctx, id,
				map[string]interface{}{"about": fmt.Sprintf("save-%d", i)},
				[]string{tagIDs[i]})
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "writer %d", i)
	}

	about := readAbout(t, pool, id)
	names := readTagNames(t, pool, id)
	var winner int
	_, scanErr := fmt.Sscanf(about, "save-%d", &winner)
	require.NoError(t, scanErr, "unexpected about value %q", about)
	assert.Equal(t, []string{fmt.Sprintf("profile-atomic-concurrent-%d", winner)}, names,
		"the surviving row text and tag set must come from the SAME save")
}

// TestUpdateProfileWithTagsLosesToAConcurrentDeletion is the soft-delete race the
// affected-row check closes: a mentor deleting their profile in one tab while a
// save is in flight in another. Whichever order the two land in, a deleted profile
// must not end up carrying the tags of a save that was refused.
func TestUpdateProfileWithTagsLosesToAConcurrentDeletion(t *testing.T) {
	repo, pool := updateTestRepo(t)
	ctx := context.Background()

	const rounds = 12
	for round := 0; round < rounds; round++ {
		id := seedMentor(t, pool, fmt.Sprintf("profile-atomic-race-%d", round))
		tagKeep := seedTag(t, pool, fmt.Sprintf("profile-atomic-race-keep-%d", round))
		tagNew := seedTag(t, pool, fmt.Sprintf("profile-atomic-race-new-%d", round))

		require.NoError(t, repo.UpdateProfileWithTags(ctx, id,
			map[string]interface{}{"about": "before"}, []string{tagKeep}))

		var wg sync.WaitGroup
		start := make(chan struct{})
		var saveErr, deleteErr error

		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			saveErr = repo.UpdateProfileWithTags(ctx, id,
				map[string]interface{}{"about": "after"}, []string{tagNew})
		}()
		go func() {
			defer wg.Done()
			<-start
			_, deleteErr = repo.SoftDeleteMentor(ctx, id)
		}()
		close(start)
		wg.Wait()

		require.NoError(t, deleteErr)

		about := readAbout(t, pool, id)
		names := readTagNames(t, pool, id)
		if saveErr == nil {
			// The save won the row: BOTH halves landed.
			assert.Equal(t, "after", about, "round %d", round)
			assert.Equal(t, []string{fmt.Sprintf("profile-atomic-race-new-%d", round)}, names,
				"round %d: a save that reported success must have written its tags too", round)
			continue
		}
		require.ErrorIs(t, saveErr, ErrMentorNotWritable, "round %d", round)
		assert.Equal(t, "before", about, "round %d", round)
		assert.Equal(t, []string{fmt.Sprintf("profile-atomic-race-keep-%d", round)}, names,
			"round %d: a refused save must leave the tag set alone", round)
	}
}

// TestCreateMentorWritesTagsInTheSameTransaction: registration used to insert the
// row, then set the tags, then only log a tags failure — a second way to produce
// the tagless profiles migration 000009 had to repair. A bad tag id must therefore
// leave NO mentor row at all.
func TestCreateMentorWritesTagsInTheSameTransaction(t *testing.T) {
	repo, pool := updateTestRepo(t)
	ctx := context.Background()
	tagID := seedTag(t, pool, "create-mentor-tag")

	fields := map[string]interface{}{
		"name":   "Tagged Registrant",
		"slug":   "create-mentor-tagged",
		"status": "draft",
	}
	mentorID, _, _, err := repo.CreateMentor(ctx, fields, []string{tagID})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM mentors WHERE id = $1`, mentorID)
	})
	assert.Equal(t, []string{"create-mentor-tag"}, readTagNames(t, pool, mentorID))

	brokenFields := map[string]interface{}{
		"name":   "Untagged Registrant",
		"slug":   "create-mentor-broken",
		"status": "draft",
	}
	t.Cleanup(func() {
		// Only reachable if the row it must not create was created anyway; the
		// cleanup keeps that failure from poisoning the next run.
		_, _ = pool.Exec(context.Background(), `DELETE FROM mentors WHERE slug = 'create-mentor-broken'`)
	})
	_, _, _, err = repo.CreateMentor(ctx, brokenFields,
		[]string{"00000000-0000-0000-0000-0000000000fe"})
	require.Error(t, err)

	var rows int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM mentors WHERE slug = 'create-mentor-broken'`).Scan(&rows))
	assert.Equal(t, 0, rows, "a registration whose tags fail must not leave a mentor row behind")
}
