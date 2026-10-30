package pipeline

import "testing"

func TestParseOutlineUnitsEnvelopeAndBare(t *testing.T) {
	units, err := parseOutlineUnits(`{"units":[{"unitId":"v01-U1","title":"新警"}]}`)
	if err != nil || len(units) != 1 || units[0].UnitID != "v01-U1" {
		t.Fatalf("envelope: %+v err=%v", units, err)
	}
	units, err = parseOutlineUnits(`{"unitId":"v01-U1","title":"黄金三章","desire":"立足"}`)
	if err != nil || len(units) != 1 || units[0].UnitID != "v01-U1" {
		t.Fatalf("bare unit: %+v err=%v", units, err)
	}
	if _, err := parseOutlineUnits(`{"units":[]}`); err == nil {
		t.Fatal("empty units should fail")
	}
}

func TestParseOutlineUnitsCoercesStoryDayObject(t *testing.T) {
	raw := `{"units":[{"unitId":"v01-U1","title":"新警","storyDay":{"day":3,"label":"入职第三天"},"chapterFrom":"1","chapterTo":3}]}`
	units, err := parseOutlineUnits(raw)
	if err != nil {
		t.Fatal(err)
	}
	if units[0].StoryDay != "3 入职第三天" {
		t.Fatalf("storyDay=%q", units[0].StoryDay)
	}
	if units[0].ChapterFrom != 1 || units[0].ChapterTo != 3 {
		t.Fatalf("chapters=%d-%d", units[0].ChapterFrom, units[0].ChapterTo)
	}
}

func TestSalvageReviewJSON(t *testing.T) {
	// Truncated mid-findings
	raw := `{"verdict":"revise","findings":[{"id":"f1","severity":"blocking","source":"ai_review","lens":"continuity","title":"时钟冲突","evidence":"story_day 倒退"},{"id":"f2","severity":"advisory","lens":"style","title":"词频","evidence":"口头禅重复`
	v, f, err := salvageReviewJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if v != "revise" || len(f) == 0 {
		t.Fatalf("verdict=%q findings=%d", v, len(f))
	}
	// JSON followed by chatter
	raw2 := `{"verdict":"pass","findings":[]}
以上是我的分析。`
	v2, f2, err2 := salvageReviewJSON(raw2)
	if err2 != nil || v2 != "pass" || f2 == nil {
		t.Fatalf("err=%v v=%q", err2, v2)
	}
}
