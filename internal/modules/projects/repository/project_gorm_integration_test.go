package repository_test

// Integration test: PostgreSQL asli + seluruh migration (lihat internal/testutil/pgtest).
// Di-skip otomatis jika TEST_DB_HOST tidak diisi.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"truss-project/internal/modules/projects/domain"
	"truss-project/internal/modules/projects/repository"
	"truss-project/internal/testutil/pgtest"
)

var ctx = context.Background()

// contextCancelled mengembalikan context yang sudah dibatalkan.
func contextCancelled() (context.Context, context.CancelFunc) {
	c, cancel := context.WithCancel(context.Background())
	cancel()
	return c, cancel
}

func setup(t *testing.T) (*repository.ProjectGormRepository, *gorm.DB) {
	t.Helper()
	db := pgtest.Setup(t)
	return repository.NewProjectGormRepository(db), db
}

func insertUser(t *testing.T, db *gorm.DB, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.Raw(
		"INSERT INTO users (name, email, password_hash) VALUES (?, ?, ?) RETURNING internal_id",
		"Tester", email, "hash",
	).Row().Scan(&id)
	if err != nil {
		t.Fatalf("gagal membuat user %s: %v", email, err)
	}
	return id
}

// insertWorkspace membuat workspace dengan owner sebagai Owner (lewat SQL, tanpa modul workspaces).
func insertWorkspace(t *testing.T, db *gorm.DB, owner uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.Raw("INSERT INTO workspaces (name, created_by) VALUES ('WS', ?) RETURNING internal_id", owner).
		Row().Scan(&id); err != nil {
		t.Fatalf("gagal membuat workspace: %v", err)
	}
	addWSMember(t, db, id, owner, "owner")
	return id
}

func addWSMember(t *testing.T, db *gorm.DB, wid, uid uuid.UUID, role string) {
	t.Helper()
	pgtest.Exec(t, db, "INSERT INTO workspace_members (workspace_id, user_id, role) VALUES (?, ?, ?)", wid, uid, role)
}

func addProjectMember(t *testing.T, db *gorm.DB, pid, uid uuid.UUID, role string) {
	t.Helper()
	pgtest.Exec(t, db, "INSERT INTO project_members (project_id, user_id, role) VALUES (?, ?, ?)", pid, uid, role)
}

func count(t *testing.T, db *gorm.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(query, args...).Scan(&n).Error; err != nil {
		t.Fatalf("count gagal: %v", err)
	}
	return n
}

func newProject(wid, creator uuid.UUID, name string) *domain.Project {
	return &domain.Project{WorkspaceID: wid, CreatedBy: creator, Name: name, Status: domain.StatusActive}
}

func mustCreate(t *testing.T, repo *repository.ProjectGormRepository, p *domain.Project) {
	t.Helper()
	if err := repo.CreateWithOwner(ctx, p); err != nil {
		t.Fatalf("CreateWithOwner: %v", err)
	}
}

// ---------- FindWorkspaceRole ----------

func TestFindWorkspaceRole(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	outsider := insertUser(t, db, "out@example.com")
	wid := insertWorkspace(t, db, owner)

	role, err := repo.FindWorkspaceRole(ctx, wid, owner)
	if err != nil || role != domain.WorkspaceOwner {
		t.Fatalf("role = %q, err = %v", role, err)
	}
	if _, err := repo.FindWorkspaceRole(ctx, wid, outsider); !errors.Is(err, domain.ErrNotWorkspaceMember) {
		t.Errorf("harapannya ErrNotWorkspaceMember, dapat %v", err)
	}
	if _, err := repo.FindWorkspaceRole(ctx, uuid.New(), owner); !errors.Is(err, domain.ErrNotWorkspaceMember) {
		t.Errorf("workspace tak ada: harapannya ErrNotWorkspaceMember, dapat %v", err)
	}
}

// ---------- CreateWithOwner ----------

func TestCreateWithOwner_PersistsProjectAndOwnerMembership(t *testing.T) {
	repo, db := setup(t)
	uid := insertUser(t, db, "a@example.com")
	wid := insertWorkspace(t, db, uid)
	desc := "isi"
	p := newProject(wid, uid, "Proyek")
	p.Description = &desc

	mustCreate(t, repo, p)

	if p.ID == uuid.Nil || p.CreatedAt.IsZero() || time.Since(p.CreatedAt) > time.Minute {
		t.Errorf("ID/timestamp tidak terisi dengan benar: %+v", p)
	}
	var name, d, status string
	var w, by uuid.UUID
	if err := db.Raw("SELECT name, description, status, workspace_id, created_by FROM projects WHERE internal_id = ?", p.ID).
		Row().Scan(&name, &d, &status, &w, &by); err != nil {
		t.Fatal(err)
	}
	if name != "Proyek" || d != "isi" || status != "active" || w != wid || by != uid {
		t.Errorf("data tersimpan salah: %q %q %q %v %v", name, d, status, w, by)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM project_members WHERE project_id = ? AND user_id = ? AND role = 'owner'", p.ID, uid); n != 1 {
		t.Errorf("keanggotaan Owner project harus tersimpan, dapat %d", n)
	}
}

func TestCreateWithOwner_NonWorkspaceMemberRejectedAndNoRows(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	outsider := insertUser(t, db, "out@example.com")
	wid := insertWorkspace(t, db, owner)

	err := repo.CreateWithOwner(ctx, newProject(wid, outsider, "Liar"))

	if !errors.Is(err, domain.ErrNotWorkspaceMember) {
		t.Fatalf("harapannya ErrNotWorkspaceMember, dapat %v", err)
	}
	if n := count(t, db, "SELECT COUNT(*) FROM projects"); n != 0 {
		t.Errorf("tidak boleh ada project tersisa, dapat %d", n)
	}
}

func TestCreateWithOwner_IsAtomic(t *testing.T) {
	repo, db := setup(t)
	uid := insertUser(t, db, "a@example.com")
	wid := insertWorkspace(t, db, uid)
	// Paksa insert keanggotaan Owner gagal.
	pgtest.Exec(t, db, "ALTER TABLE project_members DROP CONSTRAINT chk_project_members_role")
	pgtest.Exec(t, db, "ALTER TABLE project_members ADD CONSTRAINT chk_tmp CHECK (role = 'member')")

	if err := repo.CreateWithOwner(ctx, newProject(wid, uid, "Rollback")); err == nil {
		t.Fatal("harus gagal")
	}
	if n := count(t, db, "SELECT COUNT(*) FROM projects"); n != 0 {
		t.Errorf("project harus ikut di-rollback, dapat %d", n)
	}
}

func TestCreateWithOwner_SchemaRejectsBadValues(t *testing.T) {
	repo, db := setup(t)
	uid := insertUser(t, db, "a@example.com")
	wid := insertWorkspace(t, db, uid)

	if err := repo.CreateWithOwner(ctx, newProject(wid, uid, strings.Repeat("x", 101))); err == nil {
		t.Error("nama 101 karakter harus ditolak (VARCHAR(100))")
	}
	bad := newProject(wid, uid, "Status aneh")
	bad.Status = "deleted"
	if err := repo.CreateWithOwner(ctx, bad); err == nil {
		t.Error("status di luar CHECK harus ditolak")
	}
}

// Pembuatan project mengunci keanggotaan workspace pembuat (FOR SHARE). Selama
// transaksi lain memegang kunci FOR UPDATE (seperti RemoveMember modul workspaces),
// pembuatan harus menunggu; setelah keanggotaan dihapus, pembuatan ditolak.
func TestCreateWithOwner_WaitsForConcurrentMemberRemoval(t *testing.T) {
	_, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	admin := insertUser(t, db, "admin@example.com")
	wid := insertWorkspace(t, db, owner)
	addWSMember(t, db, wid, admin, "admin")

	// pgtest memakai satu koneksi (search_path schema sementara), jadi transaksi
	// "penghapus" dijalankan di koneksi kedua dengan search_path yang sama.
	var schema string
	if err := db.Raw("SELECT current_schema()").Row().Scan(&schema); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(2)
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `SET search_path TO "`+schema+`"`); err != nil {
		t.Fatal(err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Bila test gagal sebelum Commit, transaksi HARUS dilepas: tanpa ini conn.Close() menunggu
	// transaksi tanpa batas waktu dan test menggantung sampai timeout 10 menit.
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		"SELECT 1 FROM workspace_members WHERE workspace_id = $1 AND user_id = $2 FOR UPDATE", wid, admin); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		// Koneksi pool lain: perlu search_path juga, maka pakai Connection GORM.
		done <- db.Connection(func(c *gorm.DB) error {
			if err := c.Exec(`SET search_path TO "` + schema + `"`).Error; err != nil {
				return err
			}
			return repository.NewProjectGormRepository(c).CreateWithOwner(ctx, newProject(wid, admin, "Balapan"))
		})
	}()

	select {
	case err := <-done:
		t.Fatalf("pembuatan tidak menunggu kunci, selesai dengan %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM workspace_members WHERE workspace_id = $1 AND user_id = $2", wid, admin); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if !errors.Is(err, domain.ErrNotWorkspaceMember) {
			t.Fatalf("setelah dikeluarkan, harapannya ErrNotWorkspaceMember, dapat %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("pembuatan project macet")
	}
	if n := count(t, db, "SELECT COUNT(*) FROM projects"); n != 0 {
		t.Errorf("tidak boleh ada project yatim, dapat %d", n)
	}
}

// ---------- ListAccessible ----------

func TestListAccessible_VisibilityRules(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	admin := insertUser(t, db, "admin@example.com")
	member := insertUser(t, db, "member@example.com")
	plain := insertUser(t, db, "plain@example.com")
	outsider := insertUser(t, db, "out@example.com")

	w1 := insertWorkspace(t, db, owner)
	w2 := insertWorkspace(t, db, outsider) // workspace lain, owner bukan anggota
	addWSMember(t, db, w1, admin, "admin")
	addWSMember(t, db, w1, member, "member")
	addWSMember(t, db, w1, plain, "member")

	pA := newProject(w1, owner, "A")
	mustCreate(t, repo, pA)
	time.Sleep(15 * time.Millisecond)
	pB := newProject(w1, admin, "B")
	mustCreate(t, repo, pB)
	addProjectMember(t, db, pB.ID, member, "member")
	pOther := newProject(w2, outsider, "Lain")
	mustCreate(t, repo, pOther)

	ids := func(list []domain.ProjectAccess) []uuid.UUID {
		out := []uuid.UUID{}
		for _, p := range list {
			out = append(out, p.ID)
		}
		return out
	}

	tests := []struct {
		name string
		uid  uuid.UUID
		want []uuid.UUID // urut terbaru dulu
	}{
		{"owner ws melihat semua di ws-nya", owner, []uuid.UUID{pB.ID, pA.ID}},
		{"admin ws melihat semua di ws-nya", admin, []uuid.UUID{pB.ID, pA.ID}},
		{"member hanya project yang diikuti", member, []uuid.UUID{pB.ID}},
		{"member tanpa project tidak melihat apa-apa", plain, []uuid.UUID{}},
		{"owner ws lain hanya melihat miliknya", outsider, []uuid.UUID{pOther.ID}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := repo.ListAccessible(ctx, tc.uid, nil)
			if err != nil {
				t.Fatal(err)
			}
			g := ids(got)
			if len(g) != len(tc.want) {
				t.Fatalf("dapat %v, harapannya %v", g, tc.want)
			}
			for i := range g {
				if g[i] != tc.want[i] {
					t.Errorf("urutan[%d] = %v, harapannya %v", i, g[i], tc.want[i])
				}
			}
		})
	}

	t.Run("peran terpetakan", func(t *testing.T) {
		got, _ := repo.ListAccessible(ctx, admin, nil)
		roles := map[uuid.UUID]domain.Access{}
		for _, p := range got {
			roles[p.ID] = p.Access
		}
		if roles[pB.ID] != (domain.Access{WorkspaceRole: domain.WorkspaceAdmin, ProjectRole: domain.ProjectOwner}) {
			t.Errorf("akses B = %+v", roles[pB.ID])
		}
		if roles[pA.ID] != (domain.Access{WorkspaceRole: domain.WorkspaceAdmin}) {
			t.Errorf("akses A = %+v (bukan anggota project: ProjectRole kosong)", roles[pA.ID])
		}
	})

	t.Run("filter workspace", func(t *testing.T) {
		got, err := repo.ListAccessible(ctx, owner, &w2)
		if err != nil {
			t.Fatal(err)
		}
		if got == nil || len(got) != 0 {
			t.Errorf("owner bukan anggota w2: harapannya slice kosong non-nil, dapat %v", ids(got))
		}
	})

	t.Run("keanggotaan project basi tanpa keanggotaan workspace tidak terlihat", func(t *testing.T) {
		pgtest.Exec(t, db, "DELETE FROM workspace_members WHERE workspace_id = ? AND user_id = ?", w1, member)
		got, err := repo.ListAccessible(ctx, member, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Errorf("tidak boleh terlihat, dapat %v", ids(got))
		}
	})
}

// ---------- FindWithAccess ----------

func TestFindWithAccess(t *testing.T) {
	repo, db := setup(t)
	owner := insertUser(t, db, "owner@example.com")
	outsider := insertUser(t, db, "out@example.com")
	wid := insertWorkspace(t, db, owner)
	desc := "isi"
	p := newProject(wid, owner, "P")
	p.Description = &desc
	mustCreate(t, repo, p)

	got, err := repo.FindWithAccess(ctx, p.ID, owner)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != p.ID || got.WorkspaceID != wid || got.Name != "P" || got.Status != domain.StatusActive ||
		got.CreatedBy != owner || got.Description == nil || *got.Description != "isi" || got.CreatedAt.IsZero() {
		t.Errorf("field salah: %+v", got.Project)
	}
	if got.Access != (domain.Access{WorkspaceRole: domain.WorkspaceOwner, ProjectRole: domain.ProjectOwner}) {
		t.Errorf("akses = %+v", got.Access)
	}

	// Orang luar: project tetap ditemukan, tetapi aksesnya kosong (use case yang memutuskan 404).
	got, err = repo.FindWithAccess(ctx, p.ID, outsider)
	if err != nil {
		t.Fatal(err)
	}
	if got.Access != (domain.Access{}) {
		t.Errorf("orang luar harus tanpa peran, dapat %+v", got.Access)
	}

	if _, err := repo.FindWithAccess(ctx, uuid.New(), owner); !errors.Is(err, domain.ErrProjectNotFound) {
		t.Errorf("harapannya ErrProjectNotFound, dapat %v", err)
	}
}

// ---------- Update ----------

func TestUpdate(t *testing.T) {
	repo, db := setup(t)
	uid := insertUser(t, db, "a@example.com")
	wid := insertWorkspace(t, db, uid)
	desc := "lama"
	p := newProject(wid, uid, "Lama")
	p.Description = &desc
	mustCreate(t, repo, p)
	before := p.UpdatedAt
	time.Sleep(15 * time.Millisecond)

	p.Name, p.Description, p.Status = "Baru", nil, domain.StatusArchived
	if err := repo.Update(ctx, p); err != nil {
		t.Fatal(err)
	}
	if !p.UpdatedAt.After(before) {
		t.Errorf("updated_at harus maju: %v -> %v", before, p.UpdatedAt)
	}
	if n := count(t, db,
		"SELECT COUNT(*) FROM projects WHERE internal_id = ? AND name = 'Baru' AND description IS NULL AND status = 'archived'", p.ID); n != 1 {
		t.Error("perubahan tidak tersimpan")
	}

	p.Status = "deleted"
	if err := repo.Update(ctx, p); err == nil {
		t.Error("status di luar CHECK harus ditolak")
	}

	ghost := newProject(wid, uid, "Hantu")
	ghost.ID = uuid.New()
	if err := repo.Update(ctx, ghost); !errors.Is(err, domain.ErrProjectNotFound) {
		t.Errorf("harapannya ErrProjectNotFound, dapat %v", err)
	}
}

// ---------- Delete ----------

func TestDelete_CascadesMembershipsAndTasksOnlyForThatProject(t *testing.T) {
	repo, db := setup(t)
	uid := insertUser(t, db, "a@example.com")
	wid := insertWorkspace(t, db, uid)
	gone, kept := newProject(wid, uid, "Hapus"), newProject(wid, uid, "Simpan")
	mustCreate(t, repo, gone)
	mustCreate(t, repo, kept)
	for _, pid := range []uuid.UUID{gone.ID, kept.ID} {
		pgtest.Exec(t, db, "INSERT INTO tasks (project_id, title, created_by) VALUES (?, 'T', ?)", pid, uid)
	}

	if err := repo.Delete(ctx, gone.ID); err != nil {
		t.Fatal(err)
	}

	if n := count(t, db, "SELECT COUNT(*) FROM projects WHERE internal_id = ?", gone.ID); n != 0 {
		t.Error("project harus terhapus")
	}
	if n := count(t, db, "SELECT COUNT(*) FROM project_members WHERE project_id = ?", gone.ID); n != 0 {
		t.Error("keanggotaan project harus ikut terhapus")
	}
	if n := count(t, db, "SELECT COUNT(*) FROM tasks WHERE project_id = ?", gone.ID); n != 0 {
		t.Error("task project harus ikut terhapus")
	}
	if n := count(t, db, "SELECT COUNT(*) FROM tasks WHERE project_id = ?", kept.ID); n != 1 {
		t.Error("task project lain harus tetap")
	}
	if err := repo.Delete(ctx, gone.ID); !errors.Is(err, domain.ErrProjectNotFound) {
		t.Errorf("hapus kedua kali: harapannya ErrProjectNotFound, dapat %v", err)
	}
}

// ---------- context ----------

func TestCancelledContextReturnsRealError(t *testing.T) {
	repo, db := setup(t)
	uid := insertUser(t, db, "a@example.com")
	wid := insertWorkspace(t, db, uid)

	cctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := repo.FindWorkspaceRole(cctx, wid, uid); err == nil || errors.Is(err, domain.ErrNotWorkspaceMember) {
		t.Errorf("FindWorkspaceRole: dapat %v", err)
	}
	if err := repo.CreateWithOwner(cctx, newProject(wid, uid, "X")); err == nil || errors.Is(err, domain.ErrNotWorkspaceMember) {
		t.Errorf("CreateWithOwner: dapat %v", err)
	}
	if _, err := repo.FindWithAccess(cctx, uuid.New(), uid); err == nil || errors.Is(err, domain.ErrProjectNotFound) {
		t.Errorf("FindWithAccess: dapat %v", err)
	}
	ghost := newProject(wid, uid, "X")
	ghost.ID = uuid.New()
	if err := repo.Update(cctx, ghost); err == nil || errors.Is(err, domain.ErrProjectNotFound) {
		t.Errorf("Update: dapat %v", err)
	}
	if err := repo.Delete(cctx, uuid.New()); err == nil || errors.Is(err, domain.ErrProjectNotFound) {
		t.Errorf("Delete: dapat %v", err)
	}
}
