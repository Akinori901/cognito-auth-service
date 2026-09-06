package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/Akinori901/cognito-auth-service/backend/internal/entity"
)

// ErrInvalidInput は入力が受け付けられないことを表す。
// controller はこれを 400 に変換する。
var ErrInvalidInput = errors.New("入力が不正です")

// ErrAlreadyExists は entity のものを指す別名。
//
// 定義を entity に置いているのは repo と共有するため。ここで再輸出して
// おくと、controller は usecase だけを見ればよくなる。
var ErrAlreadyExists = entity.ErrAlreadyExists

// ErrNotResendable は招待の再送ができない状態。entity のものを指す。
var ErrNotResendable = entity.ErrNotResendable

// ConsoleUseCase はコンソールからの設定操作を担う。
type ConsoleUseCase struct {
	users      UserRepository
	grants     GrantRepository
	identities IdentityRepository
	cognito    CognitoDirectory
	clock      Clock
}

func NewConsoleUseCase(
	users UserRepository,
	grants GrantRepository,
	identities IdentityRepository,
	cognito CognitoDirectory,
	clock Clock,
) *ConsoleUseCase {
	return &ConsoleUseCase{
		users:      users,
		grants:     grants,
		identities: identities,
		cognito:    cognito,
		clock:      clock,
	}
}

func (uc *ConsoleUseCase) ListUsers(ctx context.Context) ([]entity.User, error) {
	return uc.users.List(ctx)
}

// SaveUser はユーザーを登録・更新する。
//
// CreatedAt は既存があれば引き継ぐ。「いつから使えるようになったか」は
// 後から辿れる必要があるため、更新のたびに上書きしない。
func (uc *ConsoleUseCase) SaveUser(ctx context.Context, u entity.User) (entity.User, error) {
	u.Email = normalizeEmail(u.Email)
	if u.Email == "" {
		return entity.User{}, fmt.Errorf("%w: email は必須です", ErrInvalidInput)
	}
	if u.Status != entity.StatusActive && u.Status != entity.StatusSuspended {
		return entity.User{}, fmt.Errorf("%w: status は active か suspended です", ErrInvalidInput)
	}

	now := uc.clock.NowRFC3339()
	existing, err := uc.users.FindByEmail(ctx, u.Email)
	if err != nil {
		return entity.User{}, err
	}
	if existing != nil {
		u.CreatedAt = existing.CreatedAt
	} else {
		u.CreatedAt = now
	}
	u.UpdatedAt = now

	if err := uc.users.Save(ctx, u); err != nil {
		return entity.User{}, err
	}
	return u, nil
}

// SaveUserResult は登録の結果。台帳の保存と Cognito 作成は
// 成否が別々になりうるため、両方を返す。
type SaveUserResult struct {
	User entity.User
	// CognitoCreated は Cognito にユーザーを作り、招待メールを送ったか。
	CognitoCreated bool
	// CognitoSkipped は既に Cognito に居たため作成しなかったか。
	CognitoSkipped bool
	// CognitoError は Cognito 作成だけが失敗したときの理由。
	// **台帳の保存は成功している**（この場合 User は有効）。
	CognitoError string
}

// SaveUserWithCognito は台帳へ登録し、必要なら Cognito にも作成する。
//
// 順序が重要で、**台帳を先に保存する**。逆にすると、招待メールを送った後に
// 台帳の保存が失敗したとき、メールだけ届いて設定が無い状態が残る。
// 台帳が先なら、Cognito 作成が失敗しても画面から作成し直せる。
//
// Cognito 作成の失敗で全体をエラーにしないのも同じ理由。ここで失敗を返すと
// 画面は「保存に失敗」と出すが台帳には入っており、利用者が実態を誤解する。
func (uc *ConsoleUseCase) SaveUserWithCognito(
	ctx context.Context, u entity.User, createCognito bool,
) (SaveUserResult, error) {
	saved, err := uc.SaveUser(ctx, u)
	if err != nil {
		return SaveUserResult{}, err
	}

	res := SaveUserResult{User: saved}
	if !createCognito || uc.cognito == nil {
		return res, nil
	}

	switch err := uc.cognito.Create(ctx, saved.Email, saved.DisplayName); {
	case err == nil:
		res.CognitoCreated = true
	case errors.Is(err, ErrAlreadyExists):
		// 片方の email だけ先に作ってあった、という状況は普通に起きる。
		// 失敗として扱うと、残りの設定まで巻き戻したくなってしまう。
		res.CognitoSkipped = true
	default:
		res.CognitoError = err.Error()
	}
	return res, nil
}

// CognitoStatusOf は台帳の各ユーザーについて Cognito 側の状態を返す。
//
// 一覧画面で「台帳にはあるが Cognito に居ない」を可視化するために使う。
// この 2 つは自動では揃わないため、ずれを見せないと気付けない。
//
// 個々の取得に失敗しても全体を落とさない。1 人分が取れないことより、
// 一覧そのものが表示できなくなる方が困る。
func (uc *ConsoleUseCase) CognitoStatusOf(ctx context.Context, emails []string) map[string]entity.CognitoAccount {
	out := make(map[string]entity.CognitoAccount, len(emails))
	if uc.cognito == nil {
		return out
	}
	for _, e := range emails {
		e = normalizeEmail(e)
		if e == "" {
			continue
		}
		acc, err := uc.cognito.Find(ctx, e)
		if err != nil {
			continue // 取れなかった分は載せない（画面は「不明」と出す）
		}
		out[e] = acc
	}
	return out
}

// CreateCognitoUser は既存の台帳ユーザーに対して Cognito 側を作る。
//
// 台帳だけ先に登録されていた人（コンソール導入前に手で入れた分など）を
// 後から救うための操作。
func (uc *ConsoleUseCase) CreateCognitoUser(ctx context.Context, email string) error {
	e := normalizeEmail(email)
	if e == "" {
		return fmt.Errorf("%w: email は必須です", ErrInvalidInput)
	}
	if uc.cognito == nil {
		return fmt.Errorf("%w: Cognito が設定されていません", ErrInvalidInput)
	}

	// 台帳に無い人を Cognito に作らない。SaveGrant と同じ理由で、
	// 層を飛び越えた状態（ログインできるが台帳に無い）を作らせない。
	u, err := uc.users.FindByEmail(ctx, e)
	if err != nil {
		return err
	}
	if u == nil {
		return fmt.Errorf("%w: 先にユーザーを登録してください", ErrInvalidInput)
	}

	return uc.cognito.Create(ctx, e, u.DisplayName)
}

// ResendInvite は招待メールを再送する。
//
// 失効していてもいなくても送れる。届いていない（迷惑メール等）ケースが
// 実際に起きるため、失効を待たせない。
//
// 再送すると **仮パスワードが変わる**。前のメールに載っていた値は
// 使えなくなるので、本人には最新のメールを見てもらう必要がある。
func (uc *ConsoleUseCase) ResendInvite(ctx context.Context, email string) error {
	e := normalizeEmail(email)
	if e == "" {
		return fmt.Errorf("%w: email は必須です", ErrInvalidInput)
	}
	if uc.cognito == nil {
		return fmt.Errorf("%w: Cognito が設定されていません", ErrInvalidInput)
	}

	// 台帳に無い人には送らない。CreateCognitoUser と同じ理由で、
	// コンソールの管理外にいる人へメールを飛ばす経路を作らない。
	u, err := uc.users.FindByEmail(ctx, e)
	if err != nil {
		return err
	}
	if u == nil {
		return fmt.Errorf("%w: 台帳に登録されていません", ErrInvalidInput)
	}

	return uc.cognito.ResendInvite(ctx, e)
}

// ListAppClients は App Client を列挙する。
//
// 「どのアプリがこのプールに登録されているか」を画面で確認するためのもの。
// 作成・削除は Terraform の担当なので、ここには読み取りしか無い。
func (uc *ConsoleUseCase) ListAppClients(ctx context.Context) ([]entity.AppClient, error) {
	if uc.cognito == nil {
		return nil, nil
	}
	return uc.cognito.ListAppClients(ctx)
}

// SetCognitoEnabled は Cognito アカウントの有効・無効を切り替える。
//
// 台帳の Status（active/suspended）とは別物。台帳の停止は各アプリの
// 認可を拒否するだけで、**Cognito のログイン自体は通る**。
// 即座にログインを止めたいときはこちらを無効にする。
func (uc *ConsoleUseCase) SetCognitoEnabled(ctx context.Context, email string, enabled bool) error {
	e := normalizeEmail(email)
	if e == "" {
		return fmt.Errorf("%w: email は必須です", ErrInvalidInput)
	}
	if uc.cognito == nil {
		return fmt.Errorf("%w: Cognito が設定されていません", ErrInvalidInput)
	}
	return uc.cognito.SetEnabled(ctx, e, enabled)
}

// DeleteUser はユーザーと、そのユーザーの全 Grant を消す。
//
// Grant を残すと「ユーザーは居ないのに許可だけある」状態になる。
// 同じ email で再登録したとき、意図しない権限が復活してしまう。
func (uc *ConsoleUseCase) DeleteUser(ctx context.Context, email string) error {
	e := normalizeEmail(email)
	if e == "" {
		return fmt.Errorf("%w: email は必須です", ErrInvalidInput)
	}

	grants, err := uc.grants.ListByEmail(ctx, e)
	if err != nil {
		return err
	}
	for _, g := range grants {
		if err := uc.grants.Delete(ctx, e, g.App); err != nil {
			return err
		}
	}
	return uc.users.Delete(ctx, e)
}

func (uc *ConsoleUseCase) ListGrants(ctx context.Context, email string) ([]entity.Grant, error) {
	return uc.grants.ListByEmail(ctx, normalizeEmail(email))
}

// SaveGrant はアプリの利用許可を作る・更新する。
//
// **ユーザーが未登録なら拒否する。** Grant だけが先にあると、
// 「乗り入れ許可はしていないのにアプリは使える」という
// 層の飛び越しが起きる。
func (uc *ConsoleUseCase) SaveGrant(ctx context.Context, g entity.Grant) (entity.Grant, error) {
	g.Email = normalizeEmail(g.Email)
	if g.Email == "" {
		return entity.Grant{}, fmt.Errorf("%w: email は必須です", ErrInvalidInput)
	}
	if !isKnownApp(g.App) {
		return entity.Grant{}, fmt.Errorf("%w: 未知のアプリです: %s", ErrInvalidInput, g.App)
	}
	if !isKnownRole(g.Role) {
		return entity.Grant{}, fmt.Errorf("%w: 未知のロールです: %s", ErrInvalidInput, g.Role)
	}

	u, err := uc.users.FindByEmail(ctx, g.Email)
	if err != nil {
		return entity.Grant{}, err
	}
	if u == nil {
		return entity.Grant{}, fmt.Errorf("%w: 先にユーザーを登録してください", ErrInvalidInput)
	}

	now := uc.clock.NowRFC3339()
	existing, err := uc.grants.Find(ctx, g.Email, g.App)
	if err != nil {
		return entity.Grant{}, err
	}
	if existing != nil {
		g.CreatedAt = existing.CreatedAt
	} else {
		g.CreatedAt = now
	}
	g.UpdatedAt = now

	if g.Scopes == nil {
		g.Scopes = []string{}
	}

	if err := uc.grants.Save(ctx, g); err != nil {
		return entity.Grant{}, err
	}
	return g, nil
}

func (uc *ConsoleUseCase) DeleteGrant(ctx context.Context, email string, app entity.AppKey) error {
	e := normalizeEmail(email)
	if e == "" {
		return fmt.Errorf("%w: email は必須です", ErrInvalidInput)
	}
	return uc.grants.Delete(ctx, e, app)
}

// RecordSignIn はログイン経路を記録する。
//
// email をキーにする設計の弱点は「email が変わると設定が孤立する」こと。
// ここに残しておけば sub から辿れる。記録に失敗しても認証は通す
// （履歴のために本人を締め出すのは本末転倒）ので、呼び出し側は
// エラーを握ってよい。
func (uc *ConsoleUseCase) RecordSignIn(ctx context.Context, sub, email, provider string) error {
	if sub == "" {
		return nil
	}
	return uc.identities.Upsert(ctx, entity.Identity{
		Sub:            sub,
		Email:          normalizeEmail(email),
		Provider:       provider,
		LastSignedInAt: uc.clock.NowRFC3339(),
	})
}

func isKnownApp(a entity.AppKey) bool {
	switch a {
	case entity.AppTaskScope, entity.AppSkillLogger, entity.AppMoneyPilot,
		entity.AppDevBranding, entity.AppFVC, entity.AppMemoryNest:
		return true
	}
	return false
}

func isKnownRole(r entity.Role) bool {
	switch r {
	case entity.RoleAdmin, entity.RoleMember, entity.RoleViewer:
		return true
	}
	return false
}
