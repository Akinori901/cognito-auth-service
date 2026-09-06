package http

import (
	"context"
	"net/http"

	"github.com/Akinori901/cognito-auth-service/backend/internal/entity"
	"github.com/go-chi/chi/v5"
)

// ScopeCandidate はコンソールがチェックボックスを描くための候補。
type ScopeCandidate struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"`
}

// ScopeSource は各アプリからスコープ候補を取る契約。repo が満たす。
type ScopeSource interface {
	Fetch(ctx context.Context, app entity.AppKey, email string) ([]ScopeCandidate, error)
}

// handleScopeCandidates は各アプリのスコープ候補を返す。
//
// 中央はスコープの意味を知らない（不透明な文字列として扱う）ので、
// 候補はアプリ側に問い合わせる。
//
// 取得に失敗しても 200 で空配列を返し、失敗した事実は unavailable で伝える。
// **候補が取れないことを設定作業の妨げにしない** — アプリの利用可否や
// ロールの設定は候補と無関係にできるべきなので、ここでエラーを返して
// 画面を止めるのは筋が悪い。
func handleScopeCandidates(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		app := entity.AppKey(chi.URLParam(req, "app"))
		if !isKnownApp(app) {
			writeJSON(w, http.StatusBadRequest, errorBody("未知のアプリです"))
			return
		}

		if d.ScopeSource == nil {
			writeJSON(w, http.StatusOK, map[string]any{"candidates": []ScopeCandidate{}})
			return
		}

		// 誰の候補かをアプリに伝える。事業のように人ごとに違う候補があるため。
		email := req.URL.Query().Get("email")

		candidates, err := d.ScopeSource.Fetch(req.Context(), app, email)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"candidates":  []ScopeCandidate{},
				"unavailable": true,
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"candidates": candidates})
	}
}

// isKnownApp は controller 側でも検証する。
//
// usecase にも同じ判定があるが、ここで弾かないと未知のアプリ名で
// 外部への問い合わせを試みることになる。
func isKnownApp(a entity.AppKey) bool {
	switch a {
	case entity.AppTaskScope, entity.AppSkillLogger, entity.AppMoneyPilot, entity.AppDevBranding:
		return true
	}
	return false
}
