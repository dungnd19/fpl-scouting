package telegram

import "testing"

func TestParseSuggestHorizon(t *testing.T) {
	cases := []struct {
		text    string
		want    int
		wantErr bool
	}{
		{"/suggest", 3, false},
		{"/suggest 1", 1, false},
		{"/suggest 2", 2, false},
		{"/suggest 3", 3, false},
		{"/suggest  2  ", 2, false},
		{"/suggest 4", 0, true},
		{"/suggest 0", 0, true},
		{"/suggest abc", 0, true},
	}

	for _, c := range cases {
		got, err := parseSuggestHorizon(c.text)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseSuggestHorizon(%q): want error, got none", c.text)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSuggestHorizon(%q): unexpected error: %v", c.text, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseSuggestHorizon(%q) = %d, want %d", c.text, got, c.want)
		}
	}
}
