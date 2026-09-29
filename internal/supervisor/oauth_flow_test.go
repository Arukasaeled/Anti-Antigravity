package supervisor

import (
	"testing"
)

func TestImportAccountsJSON(t *testing.T) {
	// Test 1: cockpit-tools format
	cockpitJSON := []byte(`{
		"version": "2.0",
		"accounts": [
			{
				"id": "test-1",
				"email": "tester1@example.com",
				"name": "Tester One"
			}
		]
	}`)
	accounts, err := ImportAccountsJSON(cockpitJSON)
	if err != nil {
		t.Fatalf("ImportAccountsJSON(cockpitJSON) failed: %v", err)
	}
	if len(accounts) == 0 {
		t.Errorf("Expected accounts to be non-empty")
	}

	// Test 2: Array format
	arrayJSON := []byte(`[
		{
			"email": "tester2@example.com",
			"name": "Tester Two",
			"refresh_token": "1//test_refresh_token"
		}
	]`)
	accounts, err = ImportAccountsJSON(arrayJSON)
	if err != nil {
		t.Fatalf("ImportAccountsJSON(arrayJSON) failed: %v", err)
	}
	if len(accounts) == 0 {
		t.Errorf("Expected accounts to be non-empty")
	}

	// Test 3: Single account object
	singleJSON := []byte(`{
		"email": "tester3@example.com",
		"name": "Tester Three"
	}`)
	accounts, err = ImportAccountsJSON(singleJSON)
	if err != nil {
		t.Fatalf("ImportAccountsJSON(singleJSON) failed: %v", err)
	}
	if len(accounts) == 0 {
		t.Errorf("Expected accounts to be non-empty")
	}

	// Test 4: GCP credentials
	gcpJSON := []byte(`{
		"installed": {
			"client_id": "testclientid12345.apps.googleusercontent.com"
		}
	}`)
	accounts, err = ImportAccountsJSON(gcpJSON)
	if err != nil {
		t.Fatalf("ImportAccountsJSON(gcpJSON) failed: %v", err)
	}
	if len(accounts) == 0 {
		t.Errorf("Expected accounts to be non-empty")
	}
}

func TestOAuthStatus(t *testing.T) {
	status := GetOAuthStatus()
	if status.Status == "" {
		t.Error("Expected initial status to not be empty")
	}
}
