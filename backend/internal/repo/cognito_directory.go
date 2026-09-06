package repo

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Akinori901/cognito-auth-service/backend/internal/entity"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
)

// CognitoDirectory は usecase.CognitoDirectory の Cognito 実装。
//
// **パスワードは一切扱わない。** 仮パスワードは Cognito が生成し、
// 招待メールで本人にだけ届く。コンソールを通ることは無い。
type CognitoDirectory struct {
	idp        *cognitoidentityprovider.Client
	userPoolID string

	// 仮パスワードの有効期間（プール設定）。一覧表示のたびに人数分
	// 問い合わせるとプール設定の取得が重なるため、一度読んで保持する。
	// 変わることは滅多にないので TTL は設けない（プロセスの寿命で十分）。
	mu       sync.RWMutex
	validity int
	loaded   bool
}

func NewCognitoDirectory(idp *cognitoidentityprovider.Client, userPoolID string) *CognitoDirectory {
	return &CognitoDirectory{idp: idp, userPoolID: userPoolID}
}

// validityDays はプールの UnusedAccountValidityDays を返す。
//
// 取得できなければ 0。呼び出し側は 0 を「不明」として扱い、
// 残日数の表示を諦める（失効を誤って断定しない）。
func (d *CognitoDirectory) validityDays(ctx context.Context) int {
	d.mu.RLock()
	if d.loaded {
		defer d.mu.RUnlock()
		return d.validity
	}
	d.mu.RUnlock()

	out, err := d.idp.DescribeUserPool(ctx, &cognitoidentityprovider.DescribeUserPoolInput{
		UserPoolId: aws.String(d.userPoolID),
	})

	d.mu.Lock()
	defer d.mu.Unlock()
	d.loaded = true // 失敗しても再試行し続けない（毎回 API を叩くのを避ける）
	if err != nil || out.UserPool == nil || out.UserPool.AdminCreateUserConfig == nil {
		d.validity = 0
		return 0
	}
	d.validity = int(out.UserPool.AdminCreateUserConfig.UnusedAccountValidityDays)
	return d.validity
}

// Find は email で Cognito ユーザーを引く。
//
// AdminGetUser ではなく ListUsers を使う。このプールは email をユーザー名に
// しているが、Google 連携で作られたユーザーは "Google_<sub>" という別名を持つ。
// email 属性で検索すれば、どちらの経路で作られていても拾える。
func (d *CognitoDirectory) Find(ctx context.Context, email string) (entity.CognitoAccount, error) {
	out, err := d.idp.ListUsers(ctx, &cognitoidentityprovider.ListUsersInput{
		UserPoolId: aws.String(d.userPoolID),
		Filter:     aws.String(fmt.Sprintf("email = %q", email)),
		Limit:      aws.Int32(10),
	})
	if err != nil {
		return entity.CognitoAccount{}, fmt.Errorf("Cognito ユーザーの検索に失敗: %w", err)
	}

	acc := entity.CognitoAccount{Email: email, Status: entity.CognitoAbsent}
	if len(out.Users) == 0 {
		// 「居ない」はエラーではない。台帳にあって Cognito に無い状態は
		// 一覧で可視化したい正常な状態なので、そのまま返す。
		return acc, nil
	}

	// 同じ email に複数ぶら下がりうる。Google 連携している人は
	// 「Google_<sub>」と「ネイティブ」の 2 レコードを持つ。
	//
	// **Google の有無を先に見る。** ネイティブだけを見て状態を決めると、
	// Google で日常的にログインしている人が、放置されたネイティブ側の
	// FORCE_CHANGE_PASSWORD を拾って「失効」と表示されてしまう。
	// 実際に使えている経路があるなら、それがその人の状態。
	var native *types.UserType
	for i := range out.Users {
		u := &out.Users[i]
		if isExternalUsername(aws.ToString(u.Username)) {
			acc.GoogleLinked = true
			continue
		}
		if native == nil {
			native = u
		}
	}

	// Google が使えるなら、それが実際のログイン経路。
	// ネイティブ側の状態（招待中・失効）は表に出さない。
	if acc.GoogleLinked {
		acc.Status = entity.CognitoConfirmed
		// 有効・無効はネイティブ側で管理する。Pre-Sign-Up が
		// ネイティブを本体として Google をリンクする作りのため、
		// 本体を無効にすればログインは止まる。
		acc.Enabled = native == nil || native.Enabled
		return acc, nil
	}

	// 以下は Google 連携が無い場合（ネイティブのみ）。
	picked := native
	if picked == nil {
		picked = &out.Users[0]
	}

	acc.Enabled = picked.Enabled
	acc.Status = toStatus(picked.UserStatus)
	// identities 属性に入る形の連携も拾う（別ユーザーとして
	// 返らないケースがあるため）。
	acc.GoogleLinked = hasIdentities(picked)
	if acc.GoogleLinked {
		acc.Status = entity.CognitoConfirmed
		return acc, nil
	}

	// 招待中のみ、失効までの計算に必要な情報を載せる。
	// 起点が UserLastModifiedDate なのは、再送すると更新されるため。
	// UserCreateDate を使うと、再送しても「失効」と出続けてしまう。
	if acc.Status == entity.CognitoInvited && picked.UserLastModifiedDate != nil {
		acc.InviteAgeDays = int(time.Since(*picked.UserLastModifiedDate).Hours() / 24)
		acc.InviteValidityDays = d.validityDays(ctx)
	}
	return acc, nil
}

// Create は招待メール付きで Cognito ユーザーを作る。
//
// email_verified を true にするのは、admin が作った時点で
// アドレスの正当性は確認済みとみなすため。false だと初回ログイン後に
// 検証コードの入力を求められ、招待の意味が薄れる。
//
// 仮パスワードは指定しない。Cognito に生成させて招待メールで本人にだけ
// 届ける（TemporaryPassword を渡すと、その値をこちらが知ることになる）。
func (d *CognitoDirectory) Create(ctx context.Context, email, displayName string) error {
	name := displayName
	if name == "" {
		// name はプール側で必須属性（cognito.tf の schema）。
		// 空だと InvalidParameterException になるため email で埋める。
		name = email
	}

	_, err := d.idp.AdminCreateUser(ctx, &cognitoidentityprovider.AdminCreateUserInput{
		UserPoolId: aws.String(d.userPoolID),
		Username:   aws.String(email),
		UserAttributes: []types.AttributeType{
			{Name: aws.String("email"), Value: aws.String(email)},
			{Name: aws.String("email_verified"), Value: aws.String("true")},
			{Name: aws.String("name"), Value: aws.String(name)},
		},
		DesiredDeliveryMediums: []types.DeliveryMediumType{types.DeliveryMediumTypeEmail},
	})
	if err != nil {
		var exists *types.UsernameExistsException
		if errors.As(err, &exists) {
			// 呼び出し側が「作らなくてよかった」と扱えるよう、
			// 障害と区別できる形にして返す。
			return fmt.Errorf("%w: %s", entity.ErrAlreadyExists, email)
		}
		return fmt.Errorf("Cognito ユーザーの作成に失敗: %w", err)
	}
	return nil
}

// ResendInvite は招待メールを再送する。
//
// MessageAction=RESEND を付けた AdminCreateUser がそのまま再送になる。
// **新しい仮パスワードが発行され、失効までの期間もリセットされる**ので、
// 失効済みのユーザーもこれで復活できる。
//
// 属性は渡さない。渡すと既存の値を上書きしうるため、再送では触らない。
func (d *CognitoDirectory) ResendInvite(ctx context.Context, email string) error {
	_, err := d.idp.AdminCreateUser(ctx, &cognitoidentityprovider.AdminCreateUserInput{
		UserPoolId:             aws.String(d.userPoolID),
		Username:               aws.String(email),
		MessageAction:          types.MessageActionTypeResend,
		DesiredDeliveryMediums: []types.DeliveryMediumType{types.DeliveryMediumTypeEmail},
	})
	if err != nil {
		var notFound *types.UserNotFoundException
		if errors.As(err, &notFound) {
			return fmt.Errorf("%w: %s", entity.ErrNotFound, email)
		}
		var invalid *types.InvalidParameterException
		if errors.As(err, &invalid) {
			// ログイン済み（CONFIRMED）などで再送できない場合ここに来る。
			// 呼び出し側が「対象外」と判断できるよう区別して返す。
			return fmt.Errorf("%w: 招待の再送ができる状態ではありません", entity.ErrNotResendable)
		}
		return fmt.Errorf("招待メールの再送に失敗: %w", err)
	}
	return nil
}

// SetEnabled は有効・無効を切り替える。
//
// 無効にすると即座にログインできなくなる。台帳の suspended と違い、
// **発行済みトークンの期限切れを待たずに新規ログインを止められる**。
func (d *CognitoDirectory) SetEnabled(ctx context.Context, email string, enabled bool) error {
	var err error
	if enabled {
		_, err = d.idp.AdminEnableUser(ctx, &cognitoidentityprovider.AdminEnableUserInput{
			UserPoolId: aws.String(d.userPoolID),
			Username:   aws.String(email),
		})
	} else {
		_, err = d.idp.AdminDisableUser(ctx, &cognitoidentityprovider.AdminDisableUserInput{
			UserPoolId: aws.String(d.userPoolID),
			Username:   aws.String(email),
		})
	}
	if err != nil {
		return fmt.Errorf("Cognito ユーザーの状態変更に失敗: %w", err)
	}
	return nil
}

// toStatus は Cognito の状態をコンソールの語彙に畳む。
//
// 細かく分けないのは、画面の用途が「作るべきか」「招待が届いているか」の
// 判断だから。RESET_REQUIRED などは "other" にまとめて実値を出さない。
func toStatus(s types.UserStatusType) entity.CognitoStatus {
	switch s {
	case types.UserStatusTypeForceChangePassword:
		return entity.CognitoInvited
	case types.UserStatusTypeConfirmed:
		return entity.CognitoConfirmed
	default:
		return entity.CognitoOther
	}
}

// isExternalUsername は外部 IdP 由来のユーザー名か。
//
// 判定は Pre-Sign-Up トリガー（lambda/presignup/index.py）と同じ規則。
// 片方だけ変えると、リンク済みなのに「未リンク」と表示されるなど食い違う。
func isExternalUsername(username string) bool {
	for _, p := range []string{"Google_", "Facebook_", "SignInWithApple_", "LoginWithAmazon_"} {
		if len(username) >= len(p) && username[:len(p)] == p {
			return true
		}
	}
	return false
}

// hasIdentities は identities 属性を持つか（外部 IdP がリンク済みか）。
func hasIdentities(u *types.UserType) bool {
	for _, a := range u.Attributes {
		if aws.ToString(a.Name) == "identities" && aws.ToString(a.Value) != "" {
			return true
		}
	}
	return false
}

// ListAppClients はこのプールの App Client を列挙する。
//
// **client_secret は取得しない。** ListUserPoolClients は secret を返さない
// ため、この経路では秘密情報がコンソールに流れることが構造的に起きない。
// confidential client かどうかは、名前からは分からないので
// DescribeUserPoolClient で 1 件ずつ確認する。
func (d *CognitoDirectory) ListAppClients(ctx context.Context) ([]entity.AppClient, error) {
	out, err := d.idp.ListUserPoolClients(ctx, &cognitoidentityprovider.ListUserPoolClientsInput{
		UserPoolId: aws.String(d.userPoolID),
		MaxResults: aws.Int32(60),
	})
	if err != nil {
		return nil, fmt.Errorf("App Client の一覧取得に失敗: %w", err)
	}

	clients := make([]entity.AppClient, 0, len(out.UserPoolClients))
	for _, c := range out.UserPoolClients {
		ac := entity.AppClient{
			ID:   aws.ToString(c.ClientId),
			Name: aws.ToString(c.ClientName),
		}
		// secret の「有無」だけを見る。値は読まないし返さない。
		// 取得に失敗しても一覧そのものは返す（表示できないより、
		// 有無が不明なだけの方が実害が小さい）。
		det, derr := d.idp.DescribeUserPoolClient(ctx, &cognitoidentityprovider.DescribeUserPoolClientInput{
			UserPoolId: aws.String(d.userPoolID),
			ClientId:   c.ClientId,
		})
		if derr == nil && det.UserPoolClient != nil {
			ac.HasSecret = aws.ToString(det.UserPoolClient.ClientSecret) != ""
		}
		clients = append(clients, ac)
	}
	return clients, nil
}
