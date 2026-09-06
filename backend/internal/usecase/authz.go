package usecase

import (
	"context"
	"strings"

	"github.com/Akinori901/cognito-auth-service/backend/internal/entity"
)

// AuthzUseCase は「この人はこのアプリを使ってよいか」を判定する。
//
// 各アプリが認証のたびに呼ぶ、この仕組みの中核。
// **判定は必ず拒否側に倒す**: 迷ったら Deny を返す。
// 開放が遅れるのは不便で済むが、締め忘れは事故になる。
type AuthzUseCase struct {
	users  UserRepository
	grants GrantRepository
}

func NewAuthzUseCase(users UserRepository, grants GrantRepository) *AuthzUseCase {
	return &AuthzUseCase{users: users, grants: grants}
}

// Authorize は email × app の認可を判定する。
//
// 判定の順序:
//  1. email が空 → 拒否（呼び出し側のバグ。通してはいけない）
//  2. ユーザーが未登録 → 拒否
//  3. ユーザーが停止中 → 拒否（Grant があっても通さない）
//  4. Grant が無い → 拒否
//  5. すべて満たす → 許可（role と scopes を返す）
//
// エラーが起きた場合も Deny を返す。DynamoDB の障害時に
// 「判定できないから通す」が最悪の挙動になるため。
func (uc *AuthzUseCase) Authorize(ctx context.Context, email string, app entity.AppKey) (entity.Authorization, error) {
	e := normalizeEmail(email)
	if e == "" {
		return entity.Deny(), nil
	}

	u, err := uc.users.FindByEmail(ctx, e)
	if err != nil {
		return entity.Deny(), err
	}
	if u == nil || !u.IsActive() {
		return entity.Deny(), nil
	}

	g, err := uc.grants.Find(ctx, e, app)
	if err != nil {
		return entity.Deny(), err
	}
	if g == nil {
		return entity.Deny(), nil
	}

	return entity.Allow(*g), nil
}

// CanUseConsole はコンソール自体を操作してよいかを判定する。
//
// 通常のアプリと違い、**Google ログインであることが必須**。
// Cognito ネイティブ（メール + パスワード）では入れない。
// App Client 側でも Google 以外を塞いでいるが、ここでも検証して
// 二重の関門にする（設定の変更ミスで開いてしまうのを防ぐ）。
//
// breakGlassEmail は緊急用の抜け道。Google IdP が使えなくなったとき、
// この email だけ Provider の検証を飛ばす。**通常は空にしておくこと。**
func (uc *AuthzUseCase) CanUseConsole(
	ctx context.Context,
	email string,
	provider string,
	breakGlassEmail string,
) (bool, error) {
	e := normalizeEmail(email)
	if e == "" {
		return false, nil
	}

	// 緊急用の抜け道。空文字なら誰にも一致しないので平時は無効。
	isBreakGlass := breakGlassEmail != "" && e == normalizeEmail(breakGlassEmail)

	if !isBreakGlass && provider != entity.ProviderGoogle {
		return false, nil
	}

	u, err := uc.users.FindByEmail(ctx, e)
	if err != nil {
		return false, err
	}
	if u == nil || !u.IsActive() {
		return false, nil
	}
	return u.IsConsoleAdmin, nil
}

// normalizeEmail は email を突き合わせ用に正規化する。
//
// Cognito から来る email は大文字小文字が混ざりうるが、
// 同じアドレスとして扱う必要がある。保存時も検索時も必ずここを通す。
func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
