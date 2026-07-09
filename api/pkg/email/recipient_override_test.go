package email

import "testing"

func TestResolveRecipient(t *testing.T) {
	tests := []struct {
		name      string
		recipient string
		override  string
		appEnv    string
		want      string
	}{
		{
			name:      "no override returns original recipient",
			recipient: "mentor@example.com",
			override:  "",
			appEnv:    "development",
			want:      "mentor@example.com",
		},
		{
			name:      "override reroutes in development",
			recipient: "mentor@example.com",
			override:  "dev@example.com",
			appEnv:    "development",
			want:      "dev@example.com",
		},
		{
			name:      "override reroutes in staging",
			recipient: "mentor@example.com",
			override:  "dev@example.com",
			appEnv:    "staging",
			want:      "dev@example.com",
		},
		{
			name:      "override reroutes when app env is empty",
			recipient: "mentor@example.com",
			override:  "dev@example.com",
			appEnv:    "",
			want:      "dev@example.com",
		},
		{
			name:      "override is ignored in production",
			recipient: "mentor@example.com",
			override:  "dev@example.com",
			appEnv:    "production",
			want:      "mentor@example.com",
		},
		{
			name:      "no override in production returns original recipient",
			recipient: "mentor@example.com",
			override:  "",
			appEnv:    "production",
			want:      "mentor@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveRecipient(tt.recipient, tt.override, tt.appEnv); got != tt.want {
				t.Errorf("ResolveRecipient(%q, %q, %q) = %q, want %q",
					tt.recipient, tt.override, tt.appEnv, got, tt.want)
			}
		})
	}
}
