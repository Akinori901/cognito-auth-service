package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/Akinori901/cognito-auth-service/backend/internal/entity"
	"github.com/Akinori901/cognito-auth-service/backend/internal/usecase"
	"github.com/go-chi/chi/v5"
)

// --- ユーザー -------------------------------------------------------------

type userBody struct {
	Email          string `json:"email"`
	DisplayName    string `json:"displayName"`
	Status         string `json:"status"`
	IsConsoleAdmin bool   `json:"isConsoleAdmin"`
	Note           string `json:"note"`
	CreatedAt      string `json:"createdAt,omitempty"`
	UpdatedAt      string `json:"updatedAt,omitempty"`

	// Cognito 側の状態。台帳とは別の関門なので、揃っているとは限らない。
	// 取得できなかった場合は空文字（画面は「不明」と出す）。
	CognitoStatus  string `json:"cognitoStatus,omitempty"`
	CognitoEnabled bool   `json:"cognitoEnabled,omitempty"`
	GoogleLinked   bool   `json:"googleLinked,omitempty"`

	// 招待の失効まわり。CognitoStatus が "invited" のときだけ意味を持つ。
	InviteExpired  bool `json:"inviteExpired,omitempty"`
	InviteDaysLeft int  `json:"inviteDaysLeft,omitempty"`
	InviteAgeDays  int  `json:"inviteAgeDays,omitempty"`
	CanResend      bool `json:"canResend,omitempty"`
}

func toUserBody(u entity.User) userBody {
	return userBody{
		Email:          u.Email,
		DisplayName:    u.DisplayName,
		Status:         string(u.Status),
		IsConsoleAdmin: u.IsConsoleAdmin,
		Note:           u.Note,
		CreatedAt:      u.CreatedAt,
		UpdatedAt:      u.UpdatedAt,
	}
}

// handleListUsers は台帳の一覧に Cognito 側の状態を重ねて返す。
//
// 2 つを 1 つの表で見せるのがこの画面の要。別々に見ていると
// 「台帳にはあるが Cognito に居ない（＝ログインできない）」に気付けない。
func handleListUsers(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		users, err := d.Console.ListUsers(req.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody("ユーザー一覧の取得に失敗しました"))
			return
		}

		emails := make([]string, 0, len(users))
		for _, u := range users {
			emails = append(emails, u.Email)
		}
		// 取得に失敗した分は欠けるだけで、一覧そのものは返す。
		accounts := d.Console.CognitoStatusOf(req.Context(), emails)

		out := make([]userBody, 0, len(users))
		for _, u := range users {
			b := toUserBody(u)
			if acc, ok := accounts[u.Email]; ok {
				b.CognitoStatus = string(acc.Status)
				b.CognitoEnabled = acc.Enabled
				b.GoogleLinked = acc.GoogleLinked
				b.InviteExpired = acc.InviteExpired()
				b.InviteDaysLeft = acc.InviteDaysLeft()
				b.InviteAgeDays = acc.InviteAgeDays
				b.CanResend = acc.CanResendInvite()
			}
			out = append(out, b)
		}
		writeJSON(w, http.StatusOK, map[string]any{"users": out})
	}
}

// saveUserBody は登録リクエスト。createCognito だけ台帳の項目ではない。
type saveUserBody struct {
	userBody
	// CreateCognito が true なら Cognito にも作成し、招待メールを送る。
	// **フォームから明示的に指定された場合だけ送る。** 既定で送ると、
	// 表示名の修正のような編集でも毎回メールが飛ぶ。
	CreateCognito bool `json:"createCognito"`
}

func handleSaveUser(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		var b saveUserBody
		if err := json.NewDecoder(req.Body).Decode(&b); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody("リクエストの形式が不正です"))
			return
		}

		res, err := d.Console.SaveUserWithCognito(req.Context(), entity.User{
			Email:          b.Email,
			DisplayName:    b.DisplayName,
			Status:         entity.UserStatus(b.Status),
			IsConsoleAdmin: b.IsConsoleAdmin,
			Note:           b.Note,
		}, b.CreateCognito)
		if err != nil {
			writeUseCaseError(w, err, "ユーザーの保存に失敗しました")
			return
		}

		// 台帳は保存できたので 200。Cognito 側の結果は本文で伝える。
		// ここを 500 にすると「保存に失敗」と出るが台帳には入っており、
		// 画面の表示と実態が食い違う。
		out := toUserBody(res.User)
		writeJSON(w, http.StatusOK, map[string]any{
			"user":           out,
			"cognitoCreated": res.CognitoCreated,
			"cognitoSkipped": res.CognitoSkipped,
			"cognitoError":   res.CognitoError,
		})
	}
}

// handleCreateCognito は台帳にいる人の Cognito アカウントを作る。
//
// 登録済みだが Cognito に居ない人（コンソール導入前の分）を後から救う経路。
func handleCreateCognito(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		email, ok := pathEmail(w, req)
		if !ok {
			return
		}
		err := d.Console.CreateCognitoUser(req.Context(), email)
		if errors.Is(err, usecase.ErrAlreadyExists) {
			// 既にあるのは失敗ではない。画面には「作成済み」と伝える。
			writeJSON(w, http.StatusOK, map[string]any{"created": false, "alreadyExists": true})
			return
		}
		if err != nil {
			writeUseCaseError(w, err, "Cognito ユーザーの作成に失敗しました")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"created": true})
	}
}

// handleSetCognitoEnabled は Cognito アカウントの有効・無効を切り替える。
//
// 台帳の suspended とは別物。台帳の停止は各アプリの認可を拒否するだけで、
// Cognito のログイン自体は通る。即座に止めたいときはこちら。
func handleSetCognitoEnabled(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		email, ok := pathEmail(w, req)
		if !ok {
			return
		}
		var b struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(req.Body).Decode(&b); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody("リクエストの形式が不正です"))
			return
		}
		if err := d.Console.SetCognitoEnabled(req.Context(), email, b.Enabled); err != nil {
			writeUseCaseError(w, err, "Cognito ユーザーの状態変更に失敗しました")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"enabled": b.Enabled})
	}
}

// handleResendInvite は招待メールを再送する。
//
// 失効の有無で分けない。届いていない（迷惑メール等）ケースが実際に多く、
// 失効を待たせる理由がないため。
func handleResendInvite(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		email, ok := pathEmail(w, req)
		if !ok {
			return
		}
		err := d.Console.ResendInvite(req.Context(), email)
		if errors.Is(err, usecase.ErrNotResendable) {
			// 障害ではなく「対象外」。400 で理由をそのまま返す。
			writeJSON(w, http.StatusBadRequest, errorBody(
				"この人には再送できません（既にログイン済み、または Google ログイン専用のアカウントです）"))
			return
		}
		if err != nil {
			writeUseCaseError(w, err, "招待メールの再送に失敗しました")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"resent": true})
	}
}

// handleListAppClients は App Client の一覧を返す。
//
// client_secret は含めない。有無だけを bool で返す。
func handleListAppClients(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		clients, err := d.Console.ListAppClients(req.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody("App Client の取得に失敗しました"))
			return
		}
		out := make([]map[string]any, 0, len(clients))
		for _, c := range clients {
			out = append(out, map[string]any{
				"id":        c.ID,
				"name":      c.Name,
				"hasSecret": c.HasSecret,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"clients": out})
	}
}

func handleDeleteUser(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		email, ok := pathEmail(w, req)
		if !ok {
			return
		}
		if err := d.Console.DeleteUser(req.Context(), email); err != nil {
			writeUseCaseError(w, err, "ユーザーの削除に失敗しました")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- 許可（アプリ利用可否とスコープ）--------------------------------------

type grantBody struct {
	Email     string   `json:"email"`
	App       string   `json:"app"`
	Role      string   `json:"role"`
	Scopes    []string `json:"scopes"`
	CreatedAt string   `json:"createdAt,omitempty"`
	UpdatedAt string   `json:"updatedAt,omitempty"`
}

func toGrantBody(g entity.Grant) grantBody {
	// nil のまま返すと JSON が null になり、フロントで .map が落ちる。
	scopes := g.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	return grantBody{
		Email:     g.Email,
		App:       string(g.App),
		Role:      string(g.Role),
		Scopes:    scopes,
		CreatedAt: g.CreatedAt,
		UpdatedAt: g.UpdatedAt,
	}
}

func handleListGrants(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		email, ok := pathEmail(w, req)
		if !ok {
			return
		}
		grants, err := d.Console.ListGrants(req.Context(), email)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody("許可一覧の取得に失敗しました"))
			return
		}
		out := make([]grantBody, 0, len(grants))
		for _, g := range grants {
			out = append(out, toGrantBody(g))
		}
		writeJSON(w, http.StatusOK, map[string]any{"grants": out})
	}
}

func handleSaveGrant(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		email, ok := pathEmail(w, req)
		if !ok {
			return
		}
		var b grantBody
		if err := json.NewDecoder(req.Body).Decode(&b); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody("リクエストの形式が不正です"))
			return
		}

		// URL の email を正とする。body 側と食い違っていても URL を採用し、
		// 「A さんの画面から B さんの権限を書き換える」経路を作らない。
		saved, err := d.Console.SaveGrant(req.Context(), entity.Grant{
			Email:  email,
			App:    entity.AppKey(b.App),
			Role:   entity.Role(b.Role),
			Scopes: b.Scopes,
		})
		if err != nil {
			writeUseCaseError(w, err, "許可の保存に失敗しました")
			return
		}
		writeJSON(w, http.StatusOK, toGrantBody(saved))
	}
}

func handleDeleteGrant(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		email, ok := pathEmail(w, req)
		if !ok {
			return
		}
		app := entity.AppKey(chi.URLParam(req, "app"))
		if err := d.Console.DeleteGrant(req.Context(), email, app); err != nil {
			writeUseCaseError(w, err, "許可の削除に失敗しました")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- 共通 -----------------------------------------------------------------

// pathEmail は URL パスから email を取り出す。
//
// email には @ や + が入るため、フロント側で encodeURIComponent されている。
// ここでデコードしないと "user%40example.com" のまま検索して見つからない。
func pathEmail(w http.ResponseWriter, req *http.Request) (string, bool) {
	raw := chi.URLParam(req, "email")
	email, err := url.PathUnescape(raw)
	if err != nil || email == "" {
		writeJSON(w, http.StatusBadRequest, errorBody("email が不正です"))
		return "", false
	}
	return email, true
}

// writeUseCaseError は usecase のエラーを HTTP に変換する。
//
// 入力の不備（400）と障害（500）を分けるのは、呼び出し側が
// 「直せば通る」のか「待つしかない」のかを判断できるようにするため。
func writeUseCaseError(w http.ResponseWriter, err error, fallback string) {
	if errors.Is(err, usecase.ErrInvalidInput) {
		writeJSON(w, http.StatusBadRequest, errorBody(err.Error()))
		return
	}
	writeJSON(w, http.StatusInternalServerError, errorBody(fallback))
}
