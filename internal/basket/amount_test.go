package basket

import "testing"

func TestAmount(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  string
		minor int64
	}{
		{"189,90", "189,90", 18990},
		{"1.299,90", "1.299,90", 129990},
		{"1299.90", "1.299,90", 129990},
		{"1299", "1.299,00", 129900},
		{"0", "0,00", 0},
		{"-1", "", 0},
		{"1.0.00", "", 0},
		{"1,9999", "", 0},
		{"", "", 0},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()

			got, err := parseAmount(tc.input)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("parsed invalid amount as %d", got)
				}

				return
			}

			if err != nil || got != tc.minor || formatAmount(got) != tc.want {
				t.Fatalf("amount = %d, %q, %v", got, formatAmount(got), err)
			}
		})
	}
}
