// Package herdr は herdr CLI から「骨格情報」（workspace の一覧・稼働状態）を
// 取得する。docs/design.md の二層構成における骨格層を担う。
//
// JSON パース（parseWorkspaces）は純関数としてテスト可能に切り出し、
// CLI 実行（ListWorkspaces）は薄いラッパとする（手動 verify 対象）。
package herdr

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// Workspace は herdr の 1 workspace の骨格情報。
type Workspace struct {
	WorkspaceID string
	Label       string
	AgentStatus string
	PaneCount   int
	TabCount    int
}

// wsListResp は `herdr workspace list` の JSON 応答（必要フィールドのみ）。
// 未知フィールドは無視され、スキーマ追加に対して頑健。
type wsListResp struct {
	Result struct {
		Workspaces []struct {
			WorkspaceID string `json:"workspace_id"`
			Label       string `json:"label"`
			AgentStatus string `json:"agent_status"`
			PaneCount   int    `json:"pane_count"`
			TabCount    int    `json:"tab_count"`
		} `json:"workspaces"`
	} `json:"result"`
}

// parseWorkspaces は `herdr workspace list` の出力を Workspace 配列に変換する。
func parseWorkspaces(data []byte) ([]Workspace, error) {
	var resp wsListResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("workspace list の JSON パースに失敗: %w", err)
	}
	out := make([]Workspace, 0, len(resp.Result.Workspaces))
	for _, w := range resp.Result.Workspaces {
		out = append(out, Workspace{
			WorkspaceID: w.WorkspaceID,
			Label:       w.Label,
			AgentStatus: w.AgentStatus,
			PaneCount:   w.PaneCount,
			TabCount:    w.TabCount,
		})
	}
	return out, nil
}

// ListWorkspaces は `herdr workspace list` を実行して骨格情報を返す。
// herdr が無い/失敗した場合はエラーを返し、呼び出し側は state のみ表示に
// フォールバックする。
func ListWorkspaces() ([]Workspace, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "herdr", "workspace", "list").Output()
	if err != nil {
		return nil, fmt.Errorf("herdr workspace list 実行に失敗: %w", err)
	}
	return parseWorkspaces(out)
}
