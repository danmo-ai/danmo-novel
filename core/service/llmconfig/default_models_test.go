package llmconfig

import "testing"

func TestBuiltInDefaultModels_IncludesKimiK31(t *testing.T) {
	models := BuiltInDefaultModels()
	if len(models) == 0 {
		t.Fatal("expected embedded default models")
	}
	want := map[string]bool{
		"k3.1-pro":           false,
		"k3.1-pro-highspeed": false,
		"kimi-k3.1-pro":      false,
		"k3":                 false,
		"k3-256k":            false,
		"deepseek-flash":     false,
	}
	for _, m := range models {
		if _, ok := want[m.Model]; ok {
			want[m.Model] = true
			if m.Model == "k3.1-pro" && m.Temperature != 1.0 {
				t.Fatalf("k3.1-pro temperature: %v", m.Temperature)
			}
			if m.Model == "k3.1-pro" && len(m.AvailableEfforts) != 3 {
				t.Fatalf("k3.1-pro efforts: %v", m.AvailableEfforts)
			}
		}
	}
	for id, found := range want {
		if !found {
			t.Errorf("missing model %q in built-in catalog", id)
		}
	}
}
