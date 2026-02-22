package booking

import "testing"

func TestRoundTime(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   int
		want int
	}{
		{in: -3, want: 5},
		{in: 0, want: 5},
		{in: 1, want: 5},
		{in: 4, want: 5},
		{in: 5, want: 5},
		{in: 6, want: 5},
		{in: 7, want: 5},
		{in: 8, want: 10},
		{in: 12, want: 10},
		{in: 13, want: 15},
		{in: 42, want: 40},
		{in: 43, want: 45},
		{in: 59, want: 60},
	}

	for _, tc := range cases {
		t.Run("in_"+strconvItoa(tc.in), func(t *testing.T) {
			t.Parallel()
			if got := RoundTime(tc.in); got != tc.want {
				t.Fatalf("RoundTime(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func strconvItoa(v int) string {
	if v == 0 {
		return "0"
	}
	sign := ""
	if v < 0 {
		sign = "neg_"
		v = -v
	}
	digits := make([]byte, 0, 12)
	for v > 0 {
		digits = append(digits, byte('0'+(v%10)))
		v /= 10
	}
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return sign + string(digits)
}
