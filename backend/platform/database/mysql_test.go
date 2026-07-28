package database

import "testing"

func TestValidateMySQLDSN(t *testing.T) {
	tests := []struct {
		name    string
		dsn     string
		wantErr bool
	}{
		{
			name:    "valid env example",
			dsn:     "root:password@tcp(127.0.0.1:3306)/yummy_crm?charset=utf8mb4&collation=utf8mb4_0900_ai_ci&parseTime=true&loc=UTC",
			wantErr: false,
		},
		{
			name:    "missing charset",
			dsn:     "user:password@tcp(127.0.0.1:3306)/crm_prospect?collation=utf8mb4_0900_ai_ci&parseTime=true&loc=UTC",
			wantErr: true,
		},
		{
			name:    "wrong charset",
			dsn:     "user:password@tcp(127.0.0.1:3306)/crm_prospect?charset=utf8&collation=utf8mb4_0900_ai_ci&parseTime=true&loc=UTC",
			wantErr: true,
		},
		{
			name:    "conflicting charset",
			dsn:     "user:password@tcp(127.0.0.1:3306)/crm_prospect?charset=utf8mb4&charset=utf8&collation=utf8mb4_0900_ai_ci&parseTime=true&loc=UTC",
			wantErr: true,
		},
		{
			name:    "parse time false",
			dsn:     "user:password@tcp(127.0.0.1:3306)/crm_prospect?charset=utf8mb4&collation=utf8mb4_0900_ai_ci&parseTime=false&loc=UTC",
			wantErr: true,
		},
		{
			name:    "local timezone",
			dsn:     "user:password@tcp(127.0.0.1:3306)/crm_prospect?charset=utf8mb4&collation=utf8mb4_0900_ai_ci&parseTime=true&loc=Local",
			wantErr: true,
		},
		{
			name:    "multi statements enabled",
			dsn:     "user:password@tcp(127.0.0.1:3306)/crm_prospect?charset=utf8mb4&collation=utf8mb4_0900_ai_ci&parseTime=true&loc=UTC&multiStatements=true",
			wantErr: true,
		},
		{
			name:    "malformed dsn",
			dsn:     "not a mysql dsn",
			wantErr: true,
		},
		{
			name:    "special password characters supported by driver",
			dsn:     "user:pa?ss@tcp(127.0.0.1:3306)/crm_prospect?charset=utf8mb4&collation=utf8mb4_0900_ai_ci&parseTime=true&loc=UTC",
			wantErr: false,
		},
		{
			name:    "duplicate canonical charset",
			dsn:     "user:password@tcp(127.0.0.1:3306)/crm_prospect?charset=utf8mb4&charset=utf8mb4&collation=utf8mb4_0900_ai_ci&parseTime=true&loc=UTC",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMySQLDSN(tt.dsn)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
