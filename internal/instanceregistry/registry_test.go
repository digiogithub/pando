package instanceregistry

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestDir overrides instancesDir for tests and returns a cleanup func.
func setupTestDir(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	orig := instancesDir
	instancesDir = dir
	return func() {
		instancesDir = orig
	}
}

func newTestEntry(id string) *Entry {
	return &Entry{
		InstanceID: id,
		Path:       "/tmp/test-workdir",
		PID:        os.Getpid(), // use current PID so liveness check passes
		PubPort:    5555,
		RPCPort:    5556,
		StartedAt:  time.Now(),
		Mode:       ModeTUI,
		IsPrimary:  true,
	}
}

func TestAnnounceAndList(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	id := uuid.New().String()
	entry := newTestEntry(id)

	err := Announce(entry)
	require.NoError(t, err)

	r := New()
	entries, err := r.List()
	require.NoError(t, err)

	found := false
	for _, e := range entries {
		if e.InstanceID == id {
			found = true
			assert.Equal(t, entry.Path, e.Path)
			assert.Equal(t, entry.PID, e.PID)
			assert.Equal(t, entry.PubPort, e.PubPort)
			assert.Equal(t, entry.RPCPort, e.RPCPort)
			assert.Equal(t, entry.Mode, e.Mode)
			assert.Equal(t, entry.IsPrimary, e.IsPrimary)
		}
	}
	assert.True(t, found, "announced entry should appear in List()")
}

func TestRevokeRemovesEntry(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	id := uuid.New().String()
	entry := newTestEntry(id)

	err := Announce(entry)
	require.NoError(t, err)

	err = Revoke(id)
	require.NoError(t, err)

	r := New()
	entries, err := r.List()
	require.NoError(t, err)

	for _, e := range entries {
		assert.NotEqual(t, id, e.InstanceID, "revoked entry must not appear in List()")
	}
}

func TestRevokeNonExistentIsNoOp(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	err := Revoke(uuid.New().String())
	require.NoError(t, err, "Revoke of non-existent entry should be a no-op")
}

func TestStaleEntryIsPruned(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	id := uuid.New().String()
	stale := &Entry{
		InstanceID: id,
		Path:       "/tmp/stale-workdir",
		PID:        999999999, // almost certainly not a real PID
		PubPort:    5557,
		RPCPort:    5558,
		StartedAt:  time.Now(),
		Mode:       ModeWebUI,
		IsPrimary:  false,
	}

	err := Announce(stale)
	require.NoError(t, err)

	// Verify the file exists before listing.
	_, err = os.Stat(entryFilePath(id))
	require.NoError(t, err, "stale entry file should exist before List()")

	r := New()
	entries, err := r.List()
	require.NoError(t, err)

	for _, e := range entries {
		assert.NotEqual(t, id, e.InstanceID, "stale entry must be pruned from List()")
	}

	// Verify the file was removed.
	_, err = os.Stat(entryFilePath(id))
	assert.True(t, os.IsNotExist(err), "stale entry file should be removed after List()")
}

func TestListByPathFilters(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	targetPath := "/tmp/target-workdir"
	otherPath := "/tmp/other-workdir"

	targetID := uuid.New().String()
	otherID := uuid.New().String()

	targetEntry := newTestEntry(targetID)
	targetEntry.Path = targetPath

	otherEntry := newTestEntry(otherID)
	otherEntry.Path = otherPath

	require.NoError(t, Announce(targetEntry))
	require.NoError(t, Announce(otherEntry))

	r := New()
	entries, err := r.ListByPath(targetPath)
	require.NoError(t, err)

	require.Len(t, entries, 1, "ListByPath should return exactly one entry for targetPath")
	assert.Equal(t, targetID, entries[0].InstanceID)
}

func TestGetReturnsEntry(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	id := uuid.New().String()
	entry := newTestEntry(id)

	require.NoError(t, Announce(entry))

	r := New()
	got, err := r.Get(id)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, id, got.InstanceID)
}

func TestGetReturnsNilForMissing(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	r := New()
	got, err := r.Get(uuid.New().String())
	require.NoError(t, err)
	assert.Nil(t, got, "Get should return nil for non-existent instanceID")
}

func TestGetPrunesStaleEntry(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	id := uuid.New().String()
	stale := &Entry{
		InstanceID: id,
		Path:       "/tmp/stale-get-workdir",
		PID:        999999999,
		PubPort:    5559,
		RPCPort:    5560,
		StartedAt:  time.Now(),
		Mode:       ModeACP,
		IsPrimary:  false,
	}

	require.NoError(t, Announce(stale))

	r := New()
	got, err := r.Get(id)
	require.NoError(t, err)
	assert.Nil(t, got, "Get should return nil for stale entry")

	_, statErr := os.Stat(entryFilePath(id))
	assert.True(t, os.IsNotExist(statErr), "stale entry file should be removed by Get()")
}

func TestMultipleInstancesSameProcess(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	ids := make([]string, 3)
	for i := range ids {
		ids[i] = uuid.New().String()
		e := newTestEntry(ids[i])
		e.PubPort = 6000 + i
		e.RPCPort = 7000 + i
		require.NoError(t, Announce(e))
	}

	r := New()
	entries, err := r.List()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(entries), 3, fmt.Sprintf("expected at least 3 entries, got %d", len(entries)))
}
