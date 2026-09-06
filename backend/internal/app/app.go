// Package app は具象を結線する組立点。
//
// **具象を知ってよいのはこの層だけ。** usecase は interface を持ち、
// repo がそれを満たす。両者を引き合わせるのがここの役目。
package app

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	apphttp "github.com/Akinori901/cognito-auth-service/backend/internal/controller/http"
	"github.com/Akinori901/cognito-auth-service/backend/internal/entity"
	"github.com/go-chi/chi/v5"
	"github.com/Akinori901/cognito-auth-service/backend/internal/repo"
	"github.com/Akinori901/cognito-auth-service/backend/internal/usecase"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

// Config は環境変数から読む設定。
type Config struct {
	Port            string
	Region          string
	UserPoolID      string
	ClientID        string
	UsersTable      string
	GrantsTable     string
	IdentitiesTable string
	// 緊急用の抜け道。Google IdP が使えなくなったときだけ設定する。
	// **通常は空にしておくこと。** 空なら誰にも一致しないので無効。
	BreakGlassEmail string

	// 各アプリのスコープ候補エンドポイント。未設定なら「候補なし」になる。
	// 個人専有のアプリ（SkillLogger / dev-branding）は設定しなくてよい。
	ScopeEndpoints map[entity.AppKey]string
	// 各アプリと共有するシークレットの置き場（SSM Parameter Store のパラメータ名）。
	// 値そのものを環境変数に持たないのは、Lambda の設定から平文が見えるのを避けるため。
	ConsoleSecretParam string
}

// LoadConfig は環境変数を読む。必須のものが欠けていればエラーにする。
//
// 起動時に落とすのは、設定漏れのまま動いて「なぜか認証が通らない」状態に
// なるのを防ぐため。特にテーブル名が空だと DynamoDB が
// 分かりにくいエラーを返す。
func LoadConfig() (Config, error) {
	c := Config{
		Port:            envOr("PORT", "8080"),
		Region:          envOr("AWS_REGION", "ap-northeast-1"),
		UserPoolID:      os.Getenv("COGNITO_USER_POOL_ID"),
		ClientID:        os.Getenv("COGNITO_CLIENT_ID"),
		UsersTable:      os.Getenv("AUTH_USERS_TABLE"),
		GrantsTable:     os.Getenv("AUTH_GRANTS_TABLE"),
		IdentitiesTable: os.Getenv("AUTH_IDENTITIES_TABLE"),
		BreakGlassEmail: os.Getenv("BREAK_GLASS_EMAIL"),
		ConsoleSecretParam: os.Getenv("CONSOLE_SECRET_PARAM"),
		ScopeEndpoints: map[entity.AppKey]string{
			entity.AppTaskScope:  os.Getenv("SCOPE_ENDPOINT_TASK_SCOPE"),
			entity.AppMoneyPilot: os.Getenv("SCOPE_ENDPOINT_MONEY_PILOT"),
			// SkillLogger / dev-branding は個人専有でスコープ概念が無いため設定しない。
		},
	}

	missing := []string{}
	for name, v := range map[string]string{
		"COGNITO_USER_POOL_ID":  c.UserPoolID,
		"COGNITO_CLIENT_ID":     c.ClientID,
		"AUTH_USERS_TABLE":      c.UsersTable,
		"AUTH_GRANTS_TABLE":     c.GrantsTable,
		"AUTH_IDENTITIES_TABLE": c.IdentitiesTable,
	} {
		if v == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("環境変数が未設定です: %v", missing)
	}
	return c, nil
}

// NewHandler は依存を結線して HTTP ハンドラを返す。
func NewHandler(ctx context.Context, c Config) (*chi.Mux, error) {
	awsCfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(c.Region))
	if err != nil {
		return nil, fmt.Errorf("AWS 設定の読み込みに失敗: %w", err)
	}
	db := dynamodb.NewFromConfig(awsCfg)
	idp := cognitoidentityprovider.NewFromConfig(awsCfg)

	users := repo.NewUserRepo(db, c.UsersTable)
	grants := repo.NewGrantRepo(db, c.GrantsTable)
	identities := repo.NewIdentityRepo(db, c.IdentitiesTable)
	directory := repo.NewCognitoDirectory(idp, c.UserPoolID)

	authz := usecase.NewAuthzUseCase(users, grants)
	console := usecase.NewConsoleUseCase(users, grants, identities, directory, systemClock{})

	verifier := repo.NewCognitoVerifier(c.Region, c.UserPoolID, c.ClientID)

	// シークレットは起動時に 1 回だけ読む。リクエストごとに読むと
	// Parameter Store の API を毎回叩くことになる。
	secret, err := repo.LoadSecureParam(ctx, awsCfg, c.ConsoleSecretParam)
	if err != nil {
		// 取れなくても起動は続ける。候補一覧が取れなくなるだけで、
		// 認可の判定そのものは動く（設定作業の妨げにしない方針と揃える）。
		log.Printf("共有シークレットを読めませんでした（候補一覧は使えません）: %v", err)
	}
	scopes := repo.NewScopeSource(c.ScopeEndpoints, secret)

	return apphttp.NewRouter(apphttp.Deps{
		Authz:           authz,
		Console:         console,
		Verifier:        verifierAdapter{v: verifier},
		ScopeSource:     scopeAdapter{s: scopes},
		BreakGlassEmail: c.BreakGlassEmail,
	}), nil
}

// verifierAdapter は repo の戻り値を controller の型に詰め替える。
//
// controller が repo を import しないための薄い橋渡し。
// 型を合わせるだけで、ロジックは持たない。
type verifierAdapter struct {
	v *repo.CognitoVerifier
}

func (a verifierAdapter) Verify(ctx context.Context, raw string) (apphttp.Verified, error) {
	t, err := a.v.Verify(ctx, raw)
	if err != nil {
		return apphttp.Verified{}, err
	}
	return apphttp.Verified{Sub: t.Sub, Email: t.Email, Provider: t.Provider}, nil
}

// systemClock は usecase.Clock の実装。テストでは固定値に差し替える。
type systemClock struct{}

func (systemClock) NowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

// scopeAdapter は repo の戻り値を controller の型に詰め替える。
// controller が repo を import しないための橋渡し（verifierAdapter と同じ理由）。
type scopeAdapter struct {
	s *repo.ScopeSource
}

func (a scopeAdapter) Fetch(ctx context.Context, app entity.AppKey, email string) ([]apphttp.ScopeCandidate, error) {
	got, err := a.s.Fetch(ctx, app, email)
	if err != nil {
		return nil, err
	}
	out := make([]apphttp.ScopeCandidate, 0, len(got))
	for _, c := range got {
		out = append(out, apphttp.ScopeCandidate{ID: c.ID, Label: c.Label, Kind: c.Kind})
	}
	return out, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
