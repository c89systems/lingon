package sessionmanage

import "testing"

func TestCreationGeometryValidation(t *testing.T) {
	for _, tc := range []struct {
		cols, rows int
		valid      bool
	}{{0, 0, true}, {80, 9, true}, {80, 0, false}, {0, 9, false}, {-1, 9, false}, {80, -1, false}, {65536, 9, false}, {80, 65536, false}} {
		req := Request{RequestID: "geometry", SessionID: "parent", Kind: "create", Cols: tc.cols, Rows: tc.rows}
		if err := Validate(req); (err == nil) != tc.valid {
			t.Errorf("%dx%d valid=%v: %v", tc.cols, tc.rows, tc.valid, err)
		}
	}
	for _, kind := range []string{"rename", "close"} {
		req := Request{RequestID: "geometry", SessionID: "parent", Kind: kind, Cols: 80, Rows: 9}
		if kind == "rename" {
			req.Name = "name"
		}
		if Validate(req) == nil {
			t.Errorf("%s accepted creation geometry", kind)
		}
	}
}
