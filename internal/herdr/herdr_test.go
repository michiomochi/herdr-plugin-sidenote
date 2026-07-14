package herdr

import "testing"

// 実際の `herdr workspace list` 出力（抜粋）を使う。
const sampleWorkspaceList = `{"id":"cli:workspace:list","result":{"type":"workspace_list","workspaces":[
{"active_tab_id":"w27:t1","agent_status":"idle","focused":false,"label":"HQ","number":1,"pane_count":1,"tab_count":1,"workspace_id":"w27"},
{"active_tab_id":"w2A:t1","agent_status":"done","focused":false,"label":"氏名突合セルフホスティング","number":3,"pane_count":3,"tab_count":2,"workspace_id":"w2A"},
{"active_tab_id":"w2F:t1","agent_status":"working","focused":false,"label":"herdr-plugin-sidenote","number":7,"pane_count":3,"tab_count":2,"workspace_id":"w2F"}
]}}`

func TestParseWorkspaces(t *testing.T) {
	ws, err := parseWorkspaces([]byte(sampleWorkspaceList))
	if err != nil {
		t.Fatalf("パース失敗: %v", err)
	}
	if len(ws) != 3 {
		t.Fatalf("workspace 数: %d", len(ws))
	}
	// 先頭
	if ws[0].WorkspaceID != "w27" || ws[0].Label != "HQ" || ws[0].AgentStatus != "idle" {
		t.Fatalf("先頭要素が不正: %+v", ws[0])
	}
	if ws[0].PaneCount != 1 || ws[0].TabCount != 1 {
		t.Fatalf("カウントが不正: %+v", ws[0])
	}
	// 日本語 label
	if ws[1].Label != "氏名突合セルフホスティング" || ws[1].AgentStatus != "done" {
		t.Fatalf("日本語 label 要素が不正: %+v", ws[1])
	}
	// 末尾
	if ws[2].WorkspaceID != "w2F" || ws[2].AgentStatus != "working" {
		t.Fatalf("末尾要素が不正: %+v", ws[2])
	}
}

func TestParseWorkspaces_Empty(t *testing.T) {
	ws, err := parseWorkspaces([]byte(`{"result":{"workspaces":[]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 0 {
		t.Fatalf("空配列のはず: %d", len(ws))
	}
}

func TestParseWorkspaces_Invalid(t *testing.T) {
	if _, err := parseWorkspaces([]byte("{not json")); err == nil {
		t.Fatal("不正 JSON はエラーにすべき")
	}
}
