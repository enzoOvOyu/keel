package schedule

import (
	"testing"
	"time"
)

func TestCronUTCLeapRangeStepsAndDaySemantics(t *testing.T) {
	for _, test := range []struct{ expression, after, want string }{
		{"*/15 8-10 * * 1-5", "2026-10-27T08:01:10Z", "2026-10-27T08:15:00Z"},
		{"0 0 29 2 *", "2026-10-27T00:00:00Z", "2028-02-29T00:00:00Z"},
		{"0 12 * * 7", "2026-10-27T00:00:00Z", "2026-11-01T12:00:00Z"},
		{"0 0 1 * 1", "2026-10-27T00:00:00Z", "2026-11-01T00:00:00Z"},
		{"5/20 * * * *", "2026-10-27T00:05:00Z", "2026-10-27T00:25:00Z"},
		{"0 0 */1 * 1", "2026-10-27T00:00:00Z", "2026-11-02T00:00:00Z"},
		{"0 0 * * *", "2026-10-27T23:59:59Z", "2026-10-28T00:00:00Z"},
	} {
		t.Run(test.expression, func(t *testing.T) {
			c, e := Parse(test.expression)
			if e != nil {
				t.Fatal(e)
			}
			after, _ := time.Parse(time.RFC3339, test.after)
			next, e := c.Next(after)
			if e != nil || next.Format(time.RFC3339) != test.want {
				t.Fatalf("got %v (%v), want %s", next, e, test.want)
			}
		})
	}
	for _, expression := range []string{"@daily", "0 +1 * * *", "0 0 * * +1", "* * * * * *", "*/0 * * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 8", "* * * * 4-1", "* * * * x", "* * * * 1,,2"} {
		if _, e := Parse(expression); e == nil {
			t.Fatalf("accepted %q", expression)
		}
	}
	c, e := Parse("0 0 31 2 *")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Next(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); e == nil {
		t.Fatal("accepted impossible February occurrence")
	}
}
