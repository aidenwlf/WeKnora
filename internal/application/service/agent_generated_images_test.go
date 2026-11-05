package service

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"testing"

	fileService "github.com/Tencent/WeKnora/internal/application/service/file"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

func pngOf(t *testing.T, shade uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: shade, A: 255})
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, img))
	return b.Bytes()
}

// generate_image references are model-controlled: only images this session
// showed may be read back, and authorization runs before storage is touched.
func TestGeneratedImagesAuthorizeReferencesBySession(t *testing.T) {
	catalog, db := newResourceCatalogForTest(t)
	require.NoError(t, db.AutoMigrate(&types.MessageArtifactRecord{}))
	require.NoError(t, db.Exec(`CREATE TABLE sessions (
 id TEXT PRIMARY KEY, tenant_id INTEGER, user_id TEXT, deleted_at DATETIME)`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE messages (
 id TEXT PRIMARY KEY, session_id TEXT, role TEXT, images TEXT, deleted_at DATETIME)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO sessions (id,tenant_id,user_id) VALUES
 ('mine',7,'alice'),('other',7,'alice'),('bobs',7,'bob')`).Error)
	memory := &webPageMemoryFiles{objects: map[string]string{}}
	fs := fileService.NewResourceCatalogFileService(memory, catalog)
	save := func(tenant uint64, shade uint8) string {
		ref, err := fs.SaveBytes(t.Context(), pngOf(t, shade), tenant, fmt.Sprintf("img-%d.png", shade), false)
		require.NoError(t, err)
		return ref
	}
	userImage, artifact, otherSession, deleted, foreignTenant, unrelated :=
		save(7, 1), save(7, 2), save(7, 3), save(7, 4), save(8, 5), save(7, 6)
	addMessage := func(id, session, role, images string) {
		require.NoError(t, db.Exec("INSERT INTO messages (id,session_id,role,images) VALUES (?,?,?,?)",
			id, session, role, images).Error)
	}
	addMessage("u1", "mine", "user", `[{"url":"`+userImage+`"},{"url":"`+foreignTenant+`"}]`)
	addMessage("a1", "mine", "assistant", "[]")
	addMessage("a2", "other", "assistant", "[]")
	for _, row := range []struct{ session, message, ref string }{
		{"mine", "a1", artifact}, {"mine", "a1", deleted}, {"other", "a2", otherSession},
	} {
		records := types.NewMessageArtifactRecords(row.session, row.message, types.MessageArtifacts{{URL: row.ref}})
		require.NoError(t, db.Create(&records).Error)
	}
	require.NoError(t, db.Exec("UPDATE message_artifacts SET deleted_at = CURRENT_TIMESTAMP WHERE url = ?",
		deleted).Error)

	storeFor := func(owner string) *agentGeneratedImages {
		return &agentGeneratedImages{
			db: db, catalog: catalog, tenantID: 7, ownerID: owner, sessionID: "mine",
			files: func(context.Context, string) (interfaces.FileService, error) { return fs, nil },
		}
	}
	store := storeFor("alice")
	produced, err := store.Save(t.Context(), pngOf(t, 7), "new.png")
	require.NoError(t, err)

	for name, ref := range map[string]string{
		"user image": userImage, "own artifact": artifact, "same-run output": produced,
	} {
		got, err := store.Read(t.Context(), ref)
		require.NoError(t, err, name)
		require.Equal(t, "image/png", got.MIMEType, name)
		require.NotEmpty(t, got.Data, name)
	}
	reads := memory.reads
	for name, ref := range map[string]string{
		"other session artifact": otherSession, "deleted artifact": deleted,
		"other tenant on my message": foreignTenant, "unrelated tenant file": unrelated,
		"raw storage path": "local://7/exports/img.png",
	} {
		_, err := store.Read(t.Context(), ref)
		require.Error(t, err, name)
	}
	require.Equal(t, reads, memory.reads, "refusals must not read storage")

	_, err = storeFor("bob").Read(t.Context(), artifact)
	require.Error(t, err, "another user of the tenant cannot borrow the session")

	_, err = storeFor("alice").Read(t.Context(), produced)
	require.Error(t, err, "an output not yet attached to an answer belongs to its own run only")
}
