// Package entity は認証コンソールの中核となる型を定義する。
//
// **この層は何も import しない。** 標準ライブラリの strings すら
// 「文字列の正規化はどこの責務か」を曖昧にするため、ここでは使わない。
// entity が外部を知った時点で、それはもうエンティティではない。
//
// 唯一の例外が errors で、層をまたいで識別されるべき番兵エラーを置くため
// だけに使う。repo と usecase の両方から参照されるので、どちらかに置くと
// 依存が逆流する。
package entity

import "errors"

// AppKey はスコープを配る対象のアプリ。
//
// 中央はこの単位で「使ってよいか」を管理する。値は各アプリのリポジトリ名に
// 揃えてあり、増えたらここに足す。
type AppKey string

const (
	AppTaskScope   AppKey = "task-scope"
	AppSkillLogger AppKey = "skilllogger"
	AppMoneyPilot  AppKey = "money-pilot"
	AppDevBranding AppKey = "dev-branding"
	AppFVC         AppKey = "fair-value-calculator"
	AppMemoryNest  AppKey = "memory-nest"
)

// Role はアプリ内での役割。
//
// 「何が見えるか」は Scopes が決め、Role は「何をしてよいか」を決める。
// 2 つを分けているのは、同じ範囲を見るが操作権限が違う人がいるため。
type Role string

const (
	RoleAdmin  Role = "admin"  // 全操作
	RoleMember Role = "member" // 閲覧 + 自分の範囲の書き込み
	RoleViewer Role = "viewer" // 閲覧のみ
)

// UserStatus はアカウントの状態。
type UserStatus string

const (
	StatusActive    UserStatus = "active"
	StatusSuspended UserStatus = "suspended"
)

// User は共通認証基盤への乗り入れを許された人。
//
// キーが Email なのは、同一人物に Cognito sub が複数ぶら下がるため
// （Google 経由と Cognito 直接）。sub をキーにすると、ログイン経路が
// 違うだけで別人扱いになってしまう。既存 4 サービスも email をキーに
// しているので、それに揃えている。
type User struct {
	Email          string
	DisplayName    string
	Status         UserStatus
	IsConsoleAdmin bool // このコンソール自体を操作してよいか
	Note           string
	CreatedAt      string
	UpdatedAt      string
}

// IsActive は利用可能な状態か。停止中のユーザーは全アプリで拒否する。
func (u User) IsActive() bool {
	return u.Status == StatusActive
}

// Grant は「この人がこのアプリを、この役割で、この範囲まで使ってよい」という許可。
//
// **Grant が存在すること自体が「アプリを使ってよい」を意味する。**
// 無効化したいときはフラグを立てるのではなく Grant を消す。
// 「存在するが無効」という状態を作ると、判定が二段になって間違えやすい。
type Grant struct {
	Email     string
	App       AppKey
	Role      Role
	Scopes    []string // 不透明な文字列。中央は意味を解釈しない
	CreatedAt string
	UpdatedAt string
}

// HasScopeLimit はレコードの絞り込みがあるか。
//
// false（Scopes が空）は「アプリは使えるが範囲の限定なし」を意味する。
// 個人専有の SkillLogger / dev-branding はこの形になる。
// **「空 = 何も見えない」ではない**ことに注意。絞り込みの有無であって、
// 許可の有無ではない（許可は Grant の存在自体が表す）。
func (g Grant) HasScopeLimit() bool {
	return len(g.Scopes) > 0
}

// Identity は Cognito のログイン経路 1 つ分。
//
// Email をキーにする設計の弱点は「email が変わると設定が孤立する」こと。
// ログインのたびに sub → email を記録しておけば、変更後も辿れる。
type Identity struct {
	Sub            string
	Email          string
	Provider       string // "Google" / "Cognito"
	LastSignedInAt string
}

// IsGoogle は Google 経由のログインか。
//
// コンソールは Google ログインのみ許可する。Cognito ネイティブ
// （メール + パスワード）は App Client 側でも塞いでいるが、
// バックエンドでも検証して二重の関門にする。
func (i Identity) IsGoogle() bool {
	return i.Provider == ProviderGoogle
}

const (
	ProviderGoogle  = "Google"
	ProviderCognito = "Cognito"
)

// Authorization は認可判定の結果。各アプリが /api/authz で受け取る。
type Authorization struct {
	Allowed bool
	Role    Role
	Scopes  []string
}

// Deny は拒否を表す結果。Role と Scopes は空のまま返す
// （拒否理由や部分的な情報を漏らさない）。
func Deny() Authorization {
	return Authorization{Allowed: false}
}

// Allow は Grant から許可の結果を組み立てる。
func Allow(g Grant) Authorization {
	return Authorization{
		Allowed: true,
		Role:    g.Role,
		Scopes:  g.Scopes,
	}
}

// CognitoStatus は Cognito 側のアカウント状態。
//
// 台帳（User）とは別に持つ。台帳にあっても Cognito に居なければログインできず、
// その逆もある。**2 つは自動では揃わない**ので、状態として区別できるようにする。
type CognitoStatus string

const (
	// CognitoAbsent は Cognito にユーザーが居ない状態。
	// 台帳に登録してもログインできないため、コンソールで作成する必要がある。
	CognitoAbsent CognitoStatus = "absent"
	// CognitoInvited は招待済みだが一度もログインしていない状態
	// （Cognito の FORCE_CHANGE_PASSWORD）。仮パスワードは既定 7 日で失効する。
	CognitoInvited CognitoStatus = "invited"
	// CognitoConfirmed はログイン実績がある通常の状態。
	CognitoConfirmed CognitoStatus = "confirmed"
	// CognitoOther は上記以外（RESET_REQUIRED など）。
	// 細分化しないのは、コンソールの用途が「作るべきか否か」の判断だから。
	CognitoOther CognitoStatus = "other"
)

// CognitoAccount は Cognito 側の 1 アカウント分の状態。
//
// **パスワードや個人情報は持たない。** コンソールが必要とするのは
// 「作成済みか」「ログインできる状態か」「Google が繋がっているか」だけ。
type CognitoAccount struct {
	Email        string
	Status       CognitoStatus
	Enabled      bool
	GoogleLinked bool // Google IdP がリンク済みか（identities の有無）

	// InviteAgeDays は招待（または最後の再送）からの経過日数。
	// Status が CognitoInvited のときだけ意味を持つ。
	//
	// 起点は UserLastModifiedDate。再送すると更新されるため、
	// **再送のたびに期限がリセットされる**のを正しく反映できる。
	InviteAgeDays int
	// InviteValidityDays はプールの UnusedAccountValidityDays。
	// 0 なら未取得（残日数を計算しない）。
	InviteValidityDays int
}

// NeedsCreation は Cognito への作成が必要か。
func (c CognitoAccount) NeedsCreation() bool {
	return c.Status == CognitoAbsent
}

// InviteExpired は招待の仮パスワードが失効しているか。
//
// 失効しても本人は「ログインできない」だけで、アカウントは残る。
// 再送すれば新しい仮パスワードが発行され、期限もリセットされる。
func (c CognitoAccount) InviteExpired() bool {
	if c.Status != CognitoInvited || c.InviteValidityDays <= 0 {
		return false
	}
	return c.InviteAgeDays >= c.InviteValidityDays
}

// InviteDaysLeft は失効までの残日数。
//
// 失効済み・招待中でない・有効期間が不明なら 0 を返す。
// **0 は「残り 0 日」ではなく「表示しない」の意味**なので、
// 呼び出し側は InviteExpired と併せて判断する。
func (c CognitoAccount) InviteDaysLeft() int {
	if c.Status != CognitoInvited || c.InviteValidityDays <= 0 {
		return 0
	}
	left := c.InviteValidityDays - c.InviteAgeDays
	if left < 0 {
		return 0
	}
	return left
}

// CanResendInvite は招待メールを再送できるか。
//
// Cognito の MessageAction=RESEND は FORCE_CHANGE_PASSWORD の
// ユーザーにしか使えない。ログイン済み（confirmed）や外部 IdP のみの
// ユーザーには送れないため、ここで弾く。
func (c CognitoAccount) CanResendInvite() bool {
	return c.Status == CognitoInvited
}

// ErrAlreadyExists は作ろうとした対象が既にあることを表す。
//
// entity に置くのは、repo と usecase の両方が参照するため。
// usecase 側に置くと repo が usecase を import することになり、
// 依存が外から内へ逆流する（層の規約違反）。
var ErrAlreadyExists = errors.New("既に存在します")

// ErrNotFound は対象が見つからないことを表す。
var ErrNotFound = errors.New("見つかりません")

// ErrNotResendable は招待の再送ができない状態であることを表す。
//
// ログイン済み（CONFIRMED）や外部 IdP のみのユーザーが該当する。
// 障害ではないので、画面には「対象外」として穏やかに伝える。
var ErrNotResendable = errors.New("再送できる状態ではありません")

// AppClient は Cognito の App Client 1 つ分。
//
// **client_secret は持たない。** コンソールは「どのアプリが登録されているか」を
// 見せるだけで、秘密情報は扱わない。secret が要るときは CLI で取得する。
type AppClient struct {
	ID   string
	Name string
	// HasSecret は confidential client か（Custom GPT など）。
	// 値そのものではなく有無だけを持つ。
	HasSecret bool
}
