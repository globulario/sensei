package impl

import "testing"

func TestUse(t *testing.T) { if Use() != "value" { t.Fatal("unexpected value") } }
