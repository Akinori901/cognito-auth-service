package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Akinori901/cognito-auth-service/backend/internal/entity"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

// ErrInvalidToken はトークンが検証を通らなかったことを表す。
//
// 理由を細分化しないのは、呼び出し側が 401 を返すだけで、
// 「署名が違う」「期限切れ」を出し分ける必要がないため。
// むしろ細かく返すと攻撃者に手がかりを与える。
var ErrInvalidToken = errors.New("トークンが無効です")

const jwksTTL = time.Hour

// CognitoVerifier は Cognito の ID トークンを検証する。
//
// アクセストークンではなく **ID トークン**を扱う。コンソールの認可には
// email と identities（IdP 種別）が必要だが、Cognito のアクセストークンには
// これらが入らないため。
type CognitoVerifier struct {
	issuer   string
	jwksURL  string
	clientID string

	mu        sync.RWMutex
	cachedSet jwk.Set
	cachedAt  time.Time
}

func NewCognitoVerifier(region, userPoolID, clientID string) *CognitoVerifier {
	issuer := fmt.Sprintf("https://cognito-idp.%s.amazonaws.com/%s", region, userPoolID)
	return &CognitoVerifier{
		issuer:   issuer,
		jwksURL:  issuer + "/.well-known/jwks.json",
		clientID: clientID,
	}
}

// VerifiedToken は検証を通ったトークンから取り出した情報。
type VerifiedToken struct {
	Sub      string
	Email    string
	Provider string // "Google" / "Cognito"
}

// Verify は ID トークンを検証し、sub / email / IdP 種別を返す。
//
// 検証する項目:
//   - 署名（JWKS で照合。kid のローテーションに追従）
//   - iss（このユーザープールが発行したものか）
//   - exp / nbf（jwt.WithValidate が見る）
//   - aud（ID トークンでは client_id ではなく aud に入る）
//   - token_use == "id"（アクセストークンを ID トークンとして使わせない）
func (v *CognitoVerifier) Verify(ctx context.Context, raw string) (VerifiedToken, error) {
	set, err := v.jwks(ctx)
	if err != nil {
		return VerifiedToken{}, fmt.Errorf("JWKS の取得に失敗: %w", err)
	}

	tok, err := jwt.ParseString(raw,
		jwt.WithKeySet(set),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.clientID),
		jwt.WithValidate(true),
	)
	if err != nil {
		return VerifiedToken{}, ErrInvalidToken
	}

	// ID トークンであることを確認する。アクセストークンには email も
	// identities も入らないため、取り違えると provider 判定が常に空になり、
	// 「Google ログインのみ」の関門が意図せず全拒否になる。
	if u, ok := tok.Get("token_use"); !ok || u != "id" {
		return VerifiedToken{}, ErrInvalidToken
	}

	out := VerifiedToken{Sub: tok.Subject(), Provider: entity.ProviderCognito}
	if e, ok := tok.Get("email"); ok {
		if s, ok := e.(string); ok {
			out.Email = s
		}
	}
	if p := providerOf(tok); p != "" {
		out.Provider = p
	}
	return out, nil
}

// providerOf は identities claim から IdP 種別を取り出す。
//
// Cognito は外部 IdP 経由のユーザーにだけ identities を付ける。
// 実測した形:
//
//	[{"providerName":"Google","providerType":"Google","primary":"true", ...}]
//
// claim が無い = Cognito ネイティブ（メール + パスワード）のログイン。
// 型が JSON 文字列で来る場合と配列で来る場合の両方があるため、両方扱う。
func providerOf(tok jwt.Token) string {
	raw, ok := tok.Get("identities")
	if !ok {
		return ""
	}

	var list []struct {
		ProviderName string `json:"providerName"`
	}

	switch v := raw.(type) {
	case string:
		// 文字列で来る場合（JSON がそのまま入っている）
		if err := json.Unmarshal([]byte(v), &list); err != nil {
			return ""
		}
	default:
		// 配列で来る場合。一度 JSON に戻してから読む
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		if err := json.Unmarshal(b, &list); err != nil {
			return ""
		}
	}

	if len(list) == 0 {
		return ""
	}
	return list[0].ProviderName
}

// jwks は公開鍵を取得する。TTL 1 時間でキャッシュする。
//
// 認証は全リクエストで走るため、毎回取りに行くとレイテンシが乗る。
// 鍵のローテーションは頻繁ではないので 1 時間で十分。
func (v *CognitoVerifier) jwks(ctx context.Context) (jwk.Set, error) {
	v.mu.RLock()
	if v.cachedSet != nil && time.Since(v.cachedAt) < jwksTTL {
		defer v.mu.RUnlock()
		return v.cachedSet, nil
	}
	v.mu.RUnlock()

	set, err := jwk.Fetch(ctx, v.jwksURL)
	if err != nil {
		return nil, err
	}

	v.mu.Lock()
	v.cachedSet, v.cachedAt = set, time.Now()
	v.mu.Unlock()
	return set, nil
}
