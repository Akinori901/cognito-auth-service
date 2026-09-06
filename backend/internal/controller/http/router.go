// Package http は HTTP の語彙を扱う唯一の層。
//
// **この層は entity と usecase にだけ依存する。** repo を知らない。
// 具象の結線は app 層が行う。
package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Akinori901/cognito-auth-service/backend/internal/entity"
	"github.com/Akinori901/cognito-auth-service/backend/internal/usecase"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Authorizer は認可判定の契約。usecase.AuthzUseCase が満たす。
type Authorizer interface {
	Authorize(ctx context.Context, email string, app entity.AppKey) (entity.Authorization, error)
	CanUseConsole(ctx context.Context, email, provider, breakGlassEmail string) (bool, error)
}

// Console はコンソールからの設定操作の契約。usecase.ConsoleUseCase が満たす。
type Console interface {
	ListUsers(ctx context.Context) ([]entity.User, error)
	SaveUser(ctx context.Context, u entity.User) (entity.User, error)
	SaveUserWithCognito(ctx context.Context, u entity.User, createCognito bool) (usecase.SaveUserResult, error)
	CognitoStatusOf(ctx context.Context, emails []string) map[string]entity.CognitoAccount
	CreateCognitoUser(ctx context.Context, email string) error
	SetCognitoEnabled(ctx context.Context, email string, enabled bool) error
	ResendInvite(ctx context.Context, email string) error
	ListAppClients(ctx context.Context) ([]entity.AppClient, error)
	DeleteUser(ctx context.Context, email string) error
	ListGrants(ctx context.Context, email string) ([]entity.Grant, error)
	SaveGrant(ctx context.Context, g entity.Grant) (entity.Grant, error)
	DeleteGrant(ctx context.Context, email string, app entity.AppKey) error
	RecordSignIn(ctx context.Context, sub, email, provider string) error
}

// Verified は検証済みトークンの中身。
type Verified struct {
	Sub      string
	Email    string
	Provider string
}

// Verifier はトークン検証の契約。
//
// repo.CognitoVerifier を直接使わないのは、controller が repo を
// import しないため（層の規約）。app 層が薄いアダプタで橋渡しする。
type Verifier interface {
	Verify(ctx context.Context, raw string) (Verified, error)
}

// Deps はルーターが必要とするもの。app 層が具象を詰めて渡す。
type Deps struct {
	Authz           Authorizer
	Console         Console
	Verifier        Verifier
	ScopeSource     ScopeSource
	BreakGlassEmail string
}

// NewRouter は HTTP ルーターを組み立てる。
//
// 戻り値を http.Handler ではなく *chi.Mux にしているのは、Lambda アダプタ
// (aws-lambda-go-api-proxy) が具象型を要求するため。
func NewRouter(d Deps) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	// ヘルスチェック。認証なしで通す（死活監視用）。
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Get("/api/authz", handleAuthz(d))

	// コンソール API。Google ログインかつコンソール管理者のみ通す。
	r.Route("/api/console", func(cr chi.Router) {
		cr.Use(consoleAuth(d))
		cr.Get("/me", handleConsoleMe())

		cr.Get("/users", handleListUsers(d))
		cr.Post("/users", handleSaveUser(d))
		cr.Delete("/users/{email}", handleDeleteUser(d))

		// Cognito 側の操作。台帳（/users）とは別の関門なので経路も分ける。
		cr.Post("/users/{email}/cognito", handleCreateCognito(d))
		cr.Post("/users/{email}/cognito/enabled", handleSetCognitoEnabled(d))
		cr.Post("/users/{email}/cognito/resend", handleResendInvite(d))

		cr.Get("/users/{email}/grants", handleListGrants(d))
		cr.Post("/users/{email}/grants", handleSaveGrant(d))
		cr.Delete("/users/{email}/grants/{app}", handleDeleteGrant(d))

		cr.Get("/apps/{app}/scope-candidates", handleScopeCandidates(d))

		// このプールに登録されている App Client の一覧（読み取りのみ）。
		// 作成・削除は Terraform の担当なので、書き込みの経路は無い。
		cr.Get("/app-clients", handleListAppClients(d))
	})

	return r
}

// handleAuthz は認可判定。各アプリが認証のたびに呼ぶ、この仕組みの中核。
//
// 呼び出し元はサーバー間なので、ユーザーの JWT ではなく email と app を
// クエリで受け取る。**この API 自体の保護はネットワーク境界で行う**
// （API Gateway の IAM 認証など）。公開エンドポイントにしてはいけない。
func handleAuthz(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		email := req.URL.Query().Get("email")
		app := entity.AppKey(req.URL.Query().Get("app"))
		if email == "" || app == "" {
			writeJSON(w, http.StatusBadRequest, errorBody("email と app は必須です"))
			return
		}

		res, err := d.Authz.Authorize(req.Context(), email, app)
		if err != nil {
			// 判定できなかったことを隠さない。呼び出し側が「拒否」と
			// 「障害」を区別できないと、DynamoDB 障害時に全員締め出された
			// のか設定漏れなのかが分からなくなる。
			writeJSON(w, http.StatusInternalServerError, errorBody("認可の判定に失敗しました"))
			return
		}
		writeJSON(w, http.StatusOK, authzResponse{
			Allowed: res.Allowed,
			Role:    string(res.Role),
			Scopes:  res.Scopes,
		})
	}
}

func handleConsoleMe() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		v := verifiedFrom(req.Context())
		writeJSON(w, http.StatusOK, map[string]string{
			"email":    v.Email,
			"provider": v.Provider,
		})
	}
}

type authzResponse struct {
	Allowed bool     `json:"allowed"`
	Role    string   `json:"role,omitempty"`
	Scopes  []string `json:"scopes,omitempty"`
}

// consoleAuth は Authorization ヘッダを検証し、コンソール権限を確認する。
func consoleAuth(d Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			raw := bearerToken(req)
			if raw == "" {
				writeJSON(w, http.StatusUnauthorized, errorBody("認証が必要です"))
				return
			}

			v, err := d.Verifier.Verify(req.Context(), raw)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, errorBody("認証に失敗しました"))
				return
			}

			ok, err := d.Authz.CanUseConsole(req.Context(), v.Email, v.Provider, d.BreakGlassEmail)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, errorBody("権限の確認に失敗しました"))
				return
			}
			if !ok {
				// 認証は通っているので 401 ではなく 403。
				// 理由は返さない（Google 以外なのか、管理者でないのかを伏せる）。
				writeJSON(w, http.StatusForbidden, errorBody("このコンソールの利用を許可されていません"))
				return
			}

			next.ServeHTTP(w, req.WithContext(withVerified(req.Context(), v)))
		})
	}
}

// --- context への検証結果の受け渡し ---------------------------------------

type ctxKey struct{}

func withVerified(ctx context.Context, v Verified) context.Context {
	return context.WithValue(ctx, ctxKey{}, v)
}

// verifiedFrom はミドルウェアが入れた検証結果を取り出す。
// consoleAuth を通ったハンドラでしか呼ばないので、無ければゼロ値でよい。
func verifiedFrom(ctx context.Context) Verified {
	v, _ := ctx.Value(ctxKey{}).(Verified)
	return v
}

// --- 共通のレスポンス -----------------------------------------------------

func bearerToken(req *http.Request) string {
	h := req.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(h, prefix))
}

func errorBody(msg string) map[string]string {
	return map[string]string{"error": msg}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	// ここで失敗してもヘッダは送信済みで、返せるものが無い。
	// エラーを握るのは意図的（ログは app 層の責務）。
	_ = json.NewEncoder(w).Encode(body)
}
