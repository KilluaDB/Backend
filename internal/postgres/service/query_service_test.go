package service

import "testing"

func TestValidateSQLQuery_RequiresLimitForSelect(t *testing.T) {
	svc := NewQueryService(nil, nil, 50)

	if err := svc.ValidateSQLQuery("SELECT * FROM users"); err == nil {
		t.Fatalf("expected error for SELECT without LIMIT, got nil")
	}

	if err := svc.ValidateSQLQuery("SELECT * FROM users LIMIT 10"); err != nil {
		t.Fatalf("expected no error for SELECT with LIMIT <= 50, got %v", err)
	}

	if err := svc.ValidateSQLQuery("SELECT * FROM users LIMIT 100"); err == nil {
		t.Fatalf("expected error for SELECT with LIMIT > 50, got nil")
	}
}

