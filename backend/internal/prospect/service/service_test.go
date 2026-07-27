package service

import (
	"context"
	"errors"
	"testing"

	authmodel "crm-prospect-simulator/backend/internal/auth/model"
	prospectmodel "crm-prospect-simulator/backend/internal/prospect/model"
	"crm-prospect-simulator/backend/internal/prospect/repository"
	"github.com/google/uuid"
)

type fakeProspectRepository struct {
	prospect prospectmodel.Prospect
	history  []prospectmodel.StatusHistory
}

type fakePlaces struct{ calls int }

func (f *fakePlaces) Search(_ context.Context, _ prospectmodel.PlaceSearchInput) ([]prospectmodel.PlaceResult, error) {
	f.calls++
	return []prospectmodel.PlaceResult{{GooglePlaceID: "place-1"}}, nil
}
func (f *fakePlaces) Detail(_ context.Context, _ string) (prospectmodel.PlaceResult, error) {
	return prospectmodel.PlaceResult{GooglePlaceID: "place-1"}, nil
}

func (f *fakeProspectRepository) ListAssigned(_ context.Context, owner uuid.UUID) ([]prospectmodel.Prospect, error) {
	if f.prospect.AssignedSalesExecutiveID != owner {
		return []prospectmodel.Prospect{}, nil
	}
	return []prospectmodel.Prospect{f.prospect}, nil
}

func (f *fakeProspectRepository) ListWon(context.Context) ([]prospectmodel.Prospect, error) {
	if f.prospect.Status == prospectmodel.StatusWon {
		return []prospectmodel.Prospect{f.prospect}, nil
	}
	return []prospectmodel.Prospect{}, nil
}

func (f *fakeProspectRepository) ListAll(context.Context) ([]prospectmodel.Prospect, error) {
	return []prospectmodel.Prospect{f.prospect}, nil
}
func (f *fakeProspectRepository) ListSalesExecutives(context.Context) ([]prospectmodel.SalesExecutive, error) {
	return []prospectmodel.SalesExecutive{}, nil
}
func (f *fakeProspectRepository) Create(_ context.Context, _ prospectmodel.SaveProspectInput, _ uuid.UUID) (prospectmodel.Prospect, error) {
	return f.prospect, nil
}
func (f *fakeProspectRepository) CheckIn(_ context.Context, prospectID, owner uuid.UUID, input prospectmodel.CheckInInput) (prospectmodel.Visit, error) {
	if f.prospect.ID != prospectID || f.prospect.AssignedSalesExecutiveID != owner {
		return prospectmodel.Visit{}, repository.ErrNotOwner
	}
	return prospectmodel.Visit{ID: uuid.New(), ProspectID: prospectID, SalesExecutiveID: owner, CheckInLatitude: input.Latitude, CheckInLongitude: input.Longitude}, nil
}
func (f *fakeProspectRepository) CheckOut(_ context.Context, prospectID, visitID, owner uuid.UUID, input prospectmodel.CheckOutInput) (prospectmodel.Visit, error) {
	if f.prospect.ID != prospectID || f.prospect.AssignedSalesExecutiveID != owner {
		return prospectmodel.Visit{}, repository.ErrNotOwner
	}
	return prospectmodel.Visit{ID: visitID, ProspectID: prospectID, SalesExecutiveID: owner}, nil
}
func (f *fakeProspectRepository) ListVisitMonitoring(_ context.Context, _ prospectmodel.VisitMonitoringFilter) ([]prospectmodel.VisitMonitoringItem, error) {
	return nil, nil
}
func (f *fakeProspectRepository) DeleteVisit(_ context.Context, _ uuid.UUID, _ uuid.UUID) error {
	return nil
}

func (f *fakeProspectRepository) FindReview(_ context.Context, id uuid.UUID) (prospectmodel.Review, error) {
	if f.prospect.ID != id {
		return prospectmodel.Review{}, repository.ErrNotFound
	}
	return prospectmodel.Review{Prospect: f.prospect, History: f.history}, nil
}

func (f *fakeProspectRepository) Transition(_ context.Context, id, owner uuid.UUID, expected, status prospectmodel.Status, notes string) (prospectmodel.Prospect, error) {
	if f.prospect.ID != id {
		return prospectmodel.Prospect{}, repository.ErrNotFound
	}
	if f.prospect.AssignedSalesExecutiveID != owner {
		return prospectmodel.Prospect{}, repository.ErrNotOwner
	}
	if f.prospect.Status != expected {
		return prospectmodel.Prospect{}, repository.ErrInvalidStatus
	}
	previous := f.prospect.Status
	f.prospect.Status = status
	f.history = append(f.history, prospectmodel.StatusHistory{FromStatus: &previous, ToStatus: status, Notes: notes})
	return f.prospect, nil
}

func TestSalesExecutiveCanMarkOwnNegotiationProspectWon(t *testing.T) {
	owner := uuid.New()
	repo := &fakeProspectRepository{prospect: prospectmodel.Prospect{ID: uuid.New(), AssignedSalesExecutiveID: owner, Status: prospectmodel.StatusNegotiation}}
	result, err := New(repo).Transition(context.Background(), Actor{UserID: owner, Role: authmodel.RoleSalesExecutive}, repo.prospect.ID, prospectmodel.StatusWon, "Commercial terms accepted")
	if err != nil || result.Status != prospectmodel.StatusWon {
		t.Fatalf("expected WON decision, result=%+v err=%v", result, err)
	}
}

func TestSalesExecutiveCanMarkAnyActiveStageLost(t *testing.T) {
	for _, stage := range prospectmodel.ActiveStatuses {
		owner := uuid.New()
		repo := &fakeProspectRepository{prospect: prospectmodel.Prospect{ID: uuid.New(), AssignedSalesExecutiveID: owner, Status: stage}}
		result, err := New(repo).Transition(context.Background(), Actor{UserID: owner, Role: authmodel.RoleSalesExecutive}, repo.prospect.ID, prospectmodel.StatusLost, "Prospect declined")
		if err != nil || result.Status != prospectmodel.StatusLost {
			t.Fatalf("stage=%s result=%+v err=%v", stage, result, err)
		}
	}
}

func TestPipelineDoesNotAllowSkippingOrEarlyWon(t *testing.T) {
	owner := uuid.New()
	for _, target := range []prospectmodel.Status{prospectmodel.StatusQualified, prospectmodel.StatusWon} {
		repo := &fakeProspectRepository{prospect: prospectmodel.Prospect{ID: uuid.New(), AssignedSalesExecutiveID: owner, Status: prospectmodel.StatusNewLead}}
		_, err := New(repo).Transition(context.Background(), Actor{UserID: owner, Role: authmodel.RoleSalesExecutive}, repo.prospect.ID, target, "note")
		if !errors.Is(err, ErrTransition) {
			t.Fatalf("target=%s expected invalid transition, got %v", target, err)
		}
	}
}

func TestTerminalDecisionsRequireNotes(t *testing.T) {
	owner := uuid.New()
	for _, tc := range []struct {
		from prospectmodel.Status
		to   prospectmodel.Status
	}{{prospectmodel.StatusNegotiation, prospectmodel.StatusWon}, {prospectmodel.StatusInterested, prospectmodel.StatusLost}} {
		repo := &fakeProspectRepository{prospect: prospectmodel.Prospect{ID: uuid.New(), AssignedSalesExecutiveID: owner, Status: tc.from}}
		_, err := New(repo).Transition(context.Background(), Actor{UserID: owner, Role: authmodel.RoleSalesExecutive}, repo.prospect.ID, tc.to, " ")
		if !errors.Is(err, ErrNotesRequired) {
			t.Fatalf("transition %s -> %s expected notes error, got %v", tc.from, tc.to, err)
		}
	}
}

func TestPipelineAdvancesOneStage(t *testing.T) {
	owner := uuid.New()
	repo := &fakeProspectRepository{prospect: prospectmodel.Prospect{ID: uuid.New(), AssignedSalesExecutiveID: owner, Status: prospectmodel.StatusNewLead}}
	result, err := New(repo).Transition(context.Background(), Actor{UserID: owner, Role: authmodel.RoleSalesExecutive}, repo.prospect.ID, prospectmodel.StatusContacted, "")
	if err != nil || result.Status != prospectmodel.StatusContacted {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestPipelineMovesBackOneStage(t *testing.T) {
	owner := uuid.New()
	repo := &fakeProspectRepository{prospect: prospectmodel.Prospect{ID: uuid.New(), AssignedSalesExecutiveID: owner, Status: prospectmodel.StatusQualified}}
	result, err := New(repo).Transition(context.Background(), Actor{UserID: owner, Role: authmodel.RoleSalesExecutive}, repo.prospect.ID, prospectmodel.StatusInterested, "Correction")
	if err != nil || result.Status != prospectmodel.StatusInterested {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestWonProspectIsTerminalForSalesPipeline(t *testing.T) {
	owner := uuid.New()
	repo := &fakeProspectRepository{prospect: prospectmodel.Prospect{ID: uuid.New(), AssignedSalesExecutiveID: owner, Status: prospectmodel.StatusWon}}
	_, err := New(repo).Transition(context.Background(), Actor{UserID: owner, Role: authmodel.RoleSalesExecutive}, repo.prospect.ID, prospectmodel.StatusNegotiation, "Reopen")
	if !errors.Is(err, ErrTransition) {
		t.Fatalf("expected WON to be terminal, got %v", err)
	}
}

func TestVisitRequiresOwnerAndValidCoordinates(t *testing.T) {
	owner := uuid.New()
	repo := &fakeProspectRepository{prospect: prospectmodel.Prospect{ID: uuid.New(), AssignedSalesExecutiveID: owner}}
	_, err := New(repo).CheckIn(context.Background(), Actor{UserID: owner, Role: authmodel.RoleSalesExecutive}, repo.prospect.ID, prospectmodel.CheckInInput{Latitude: 100, Longitude: 106})
	if !errors.Is(err, ErrVisitCoordinates) {
		t.Fatalf("expected coordinates error, got %v", err)
	}
	_, err = New(repo).CheckIn(context.Background(), Actor{UserID: uuid.New(), Role: authmodel.RoleSalesExecutive}, repo.prospect.ID, prospectmodel.CheckInInput{Latitude: -6.2, Longitude: 106.8})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}
}

func TestSalesExecutiveCannotDecideAnotherOwnersProspect(t *testing.T) {
	repo := &fakeProspectRepository{prospect: prospectmodel.Prospect{ID: uuid.New(), AssignedSalesExecutiveID: uuid.New(), Status: prospectmodel.StatusNegotiation}}
	_, err := New(repo).Transition(context.Background(), Actor{UserID: uuid.New(), Role: authmodel.RoleSalesExecutive}, repo.prospect.ID, prospectmodel.StatusWon, "Won")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}
}

func TestWonProspectCanBeReviewedByAdministrator(t *testing.T) {
	repo := &fakeProspectRepository{prospect: prospectmodel.Prospect{ID: uuid.New(), Status: prospectmodel.StatusWon}}
	review, err := New(repo).Review(context.Background(), Actor{UserID: uuid.New(), Role: authmodel.RoleAdministrator}, repo.prospect.ID)
	if err != nil || review.Prospect.Status != prospectmodel.StatusWon {
		t.Fatalf("expected won review, result=%+v err=%v", review, err)
	}
}

func TestProspectFinderValidatesRadiusBeforeProviderCall(t *testing.T) {
	repo := &fakeProspectRepository{}
	places := &fakePlaces{}
	_, err := New(repo, places).SearchPlaces(context.Background(), Actor{UserID: uuid.New(), Role: authmodel.RoleAdministrator}, prospectmodel.PlaceSearchInput{Latitude: -6.2, Longitude: 106.8, Radius: 10})
	if !errors.Is(err, ErrFinderInput) || places.calls != 0 {
		t.Fatalf("expected validation before provider call, calls=%d err=%v", places.calls, err)
	}
}

func TestSalesRoleCannotUseProspectFinder(t *testing.T) {
	_, err := New(&fakeProspectRepository{}, &fakePlaces{}).SearchPlaces(context.Background(), Actor{UserID: uuid.New(), Role: authmodel.RoleSalesExecutive}, prospectmodel.PlaceSearchInput{Latitude: -6.2, Longitude: 106.8, Radius: 3000})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}
}
