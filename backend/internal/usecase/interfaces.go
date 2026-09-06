// Package usecase は認可の判定とコンソールの操作を担う。
//
// **この層は entity のみに依存する。** repo も controller も知らない。
// 外部技術（DynamoDB / Cognito）が必要な処理は、ここで interface として
// 契約を定義し、repo 側がそれを「満たす」（依存性逆転）。
//
// repo から usecase を import してはいけない。import した時点で
// 依存が外から内へ逆流し、この層をテストするために DynamoDB が要るようになる。
package usecase

import (
	"context"

	"github.com/Akinori901/cognito-auth-service/backend/internal/entity"
)

// UserRepository は中央のユーザー台帳への読み書き。
type UserRepository interface {
	// FindByEmail は email でユーザーを引く。見つからなければ (nil, nil)。
	//
	// 「見つからない」をエラーにしないのは、認可判定において
	// 未登録は正常な入力だから。エラーと区別できないと、
	// DynamoDB の障害と未登録を取り違える。
	FindByEmail(ctx context.Context, email string) (*entity.User, error)
	Save(ctx context.Context, u entity.User) error
	Delete(ctx context.Context, email string) error
	List(ctx context.Context) ([]entity.User, error)
}

// GrantRepository はアプリ利用可否とスコープへの読み書き。
type GrantRepository interface {
	// Find は (email, app) の許可を引く。無ければ (nil, nil)。
	Find(ctx context.Context, email string, app entity.AppKey) (*entity.Grant, error)
	// ListByEmail はそのユーザーの全アプリ分の許可を返す。
	ListByEmail(ctx context.Context, email string) ([]entity.Grant, error)
	Save(ctx context.Context, g entity.Grant) error
	Delete(ctx context.Context, email string, app entity.AppKey) error
}

// IdentityRepository は Cognito のログイン経路の記録。
type IdentityRepository interface {
	Upsert(ctx context.Context, i entity.Identity) error
	ListByEmail(ctx context.Context, email string) ([]entity.Identity, error)
}

// Clock は現在時刻。テストで固定するために注入する。
type Clock interface {
	NowRFC3339() string
}

// CognitoDirectory は Cognito ユーザープールへの操作。
//
// 台帳（UserRepository）とは別の関門を扱う。台帳に載っていても
// Cognito に居なければログインできないため、コンソールから両方を
// 面倒見られるようにする契約。
//
// **削除は意図的に持たせていない。** Cognito ユーザーを消すと Google の
// リンクも失われ、同じ email で作り直しても別人扱いになる。無効化
// （SetEnabled）で足りる用途に限定する。
type CognitoDirectory interface {
	// Find は email で Cognito ユーザーを引く。居なければ
	// Status = CognitoAbsent を返す（エラーにしない）。
	//
	// 「居ない」は一覧表示における正常な状態であり、
	// 障害と区別できないと「取得に失敗」で画面全体が落ちる。
	Find(ctx context.Context, email string) (entity.CognitoAccount, error)
	// Create は招待メール付きでユーザーを作る。既に居る場合は
	// ErrAlreadyExists を返す。
	Create(ctx context.Context, email, displayName string) error
	// SetEnabled は有効・無効を切り替える。無効にすると即座にログインできなくなる。
	SetEnabled(ctx context.Context, email string, enabled bool) error
	// ResendInvite は招待メールを再送する。仮パスワードが再発行され、
	// 失効までの期間もリセットされる。
	ResendInvite(ctx context.Context, email string) error
	// ListAppClients はこのプールの App Client を列挙する。
	//
	// 作成・削除は持たせない。client_secret を安全に渡す方法が無く、
	// 削除は当該アプリのログインを即座に全停止させるため。
	// App Client の増減は Terraform の担当。
	ListAppClients(ctx context.Context) ([]entity.AppClient, error)
}
