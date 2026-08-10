package s3storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The trash key layout is a contract with two parties outside this package:
// the operator's lifecycle rule matches on the literal "deleted/" prefix, and
// RestoreImagesFromTrash has to find exactly what MoveImagesToTrash wrote.
func TestTrashKeyLayout(t *testing.T) {
	assert.Equal(t, "deleted/abc-123/full", trashKey("abc-123/full"))

	// Every size a photo can exist under is covered by the move: the canonical
	// object UploadProfileImage writes, plus the byte-identical large/small
	// copies uploads wrote before C5.
	assert.Equal(t,
		[]string{"abc-123/full", "abc-123/large", "abc-123/small"},
		imageKeys("abc-123"),
	)
}

// A round trip must land on the original key, or a restored profile points at
// nothing.
func TestTrashKeyRoundTrips(t *testing.T) {
	for _, key := range imageKeys("mentor-uuid") {
		trashed := trashKey(key)
		assert.Equal(t, TrashPrefix+key, trashed)
		assert.Equal(t, key, trashed[len(TrashPrefix):])
	}
}
