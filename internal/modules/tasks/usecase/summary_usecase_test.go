package usecase_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"truss-project/internal/modules/tasks/domain"
	"truss-project/internal/modules/tasks/usecase"
)

var clock = time.Date(2026, 3, 1, 12, 0, 0, 0, time.FixedZone("WIB", 7*3600))

func withClock(f *fixture) {
	f.uc = usecase.NewTaskUseCase(f.repo, nil).WithClock(func() time.Time { return clock })
}

func TestSummary_CountsAndAsOfFromClock(t *testing.T) {
	f := newFixture()
	withClock(f)
	past := clock.Add(-time.Hour)
	add := func(s domain.Status, due *time.Time) {
		id := uuid.New()
		f.repo.tasks[id] = domain.Task{ID: id, ProjectID: f.pid, Status: s, DueDate: due}
	}
	add(domain.StatusDone, &past)       // selesai: tidak terlambat
	add(domain.StatusInProgress, &past) // terlambat
	add(domain.StatusTodo, nil)

	s, err := f.uc.Summary(ctx, f.bystander.String(), f.pid.String())
	if err != nil {
		t.Fatal(err)
	}
	if s.Total != 4 || s.Done != 1 || s.Overdue != 1 { // + 1 task bawaan fixture
		t.Errorf("ringkasan = %+v", s)
	}
	if !f.repo.lastAsOf.Equal(clock) || f.repo.lastAsOf.Location() != time.UTC {
		t.Errorf("acuan waktu harus dari clock, dalam UTC: %v", f.repo.lastAsOf)
	}
	if !s.AsOf.Equal(clock) {
		t.Errorf("AsOf respons = %v", s.AsOf)
	}
}

func TestSummary_AccessAndArchived(t *testing.T) {
	f := newFixture()
	for name, uid := range map[string]uuid.UUID{"owner ws": f.wsOwner, "member project": f.bystander} {
		if _, err := f.uc.Summary(ctx, uid.String(), f.pid.String()); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, uid := range map[string]uuid.UUID{"member ws saja": f.plain, "orang luar": f.outsider} {
		if _, err := f.uc.Summary(ctx, uid.String(), f.pid.String()); !errors.Is(err, domain.ErrProjectNotFound) {
			t.Errorf("%s harus 404 (PRD FR-09 #4), dapat %v", name, err)
		}
	}
	if f.repo.summaryCalls != 2 {
		t.Errorf("ringkasan tidak boleh dihitung untuk yang tak berhak, calls=%d", f.repo.summaryCalls)
	}

	f.repo.archived = true
	if _, err := f.uc.Summary(ctx, f.wsOwner.String(), f.pid.String()); err != nil {
		t.Errorf("project diarsipkan tetap bisa diringkas: %v", err)
	}
}

func TestSummary_InvalidIDsAndErrors(t *testing.T) {
	f := newFixture()
	if _, err := f.uc.Summary(ctx, "x", f.pid.String()); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Errorf("dapat %v", err)
	}
	if _, err := f.uc.Summary(ctx, f.wsOwner.String(), "x"); !errors.Is(err, domain.ErrProjectNotFound) {
		t.Errorf("dapat %v", err)
	}
	if _, err := f.uc.Summary(ctx, f.wsOwner.String(), uuid.NewString()); !errors.Is(err, domain.ErrProjectNotFound) {
		t.Errorf("dapat %v", err)
	}
	boom := errors.New("db mati")
	f.repo.summaryErr = boom
	if _, err := f.uc.Summary(ctx, f.wsOwner.String(), f.pid.String()); !errors.Is(err, boom) {
		t.Errorf("dapat %v", err)
	}
}

func TestList_OverdueFilterUsesSameClock(t *testing.T) {
	f := newFixture()
	withClock(f)

	if _, err := f.uc.List(ctx, f.bystander.String(), f.pid.String(), usecase.ListInput{Overdue: true}); err != nil {
		t.Fatal(err)
	}
	got := f.repo.lastFilter.OverdueAsOf
	if got == nil || !got.Equal(clock) || got.Location() != time.UTC {
		t.Errorf("OverdueAsOf = %v, harapannya waktu clock dalam UTC", got)
	}

	if _, err := f.uc.List(ctx, f.bystander.String(), f.pid.String(), usecase.ListInput{}); err != nil {
		t.Fatal(err)
	}
	if f.repo.lastFilter.OverdueAsOf != nil {
		t.Error("tanpa overdue=true, filter terlambat tidak boleh aktif")
	}
}
