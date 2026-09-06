package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Akinori901/cognito-auth-service/backend/internal/entity"
	"github.com/Akinori901/cognito-auth-service/backend/internal/usecase"
)

// --- 追加のフェイク -------------------------------------------------------

type fakeIdentities struct {
	saved []entity.Identity
}

func (f *fakeIdentities) Upsert(_ context.Context, i entity.Identity) error {
	f.saved = append(f.saved, i)
	return nil
}
func (f *fakeIdentities) ListByEmail(context.Context, string) ([]entity.Identity, error) {
	return nil, nil
}

type fixedClock struct{ now string }

func (c fixedClock) NowRFC3339() string { return c.now }

// 書き込みを記録する版のフェイク（authz_test のものは読み取り専用）
type recordingUsers struct {
	m       map[string]entity.User
	saved   []entity.User
	deleted []string
}

func (f *recordingUsers) FindByEmail(_ context.Context, email string) (*entity.User, error) {
	u, ok := f.m[email]
	if !ok {
		return nil, nil
	}
	return &u, nil
}
func (f *recordingUsers) Save(_ context.Context, u entity.User) error {
	f.saved = append(f.saved, u)
	if f.m == nil {
		f.m = map[string]entity.User{}
	}
	f.m[u.Email] = u
	return nil
}
func (f *recordingUsers) Delete(_ context.Context, email string) error {
	f.deleted = append(f.deleted, email)
	delete(f.m, email)
	return nil
}
func (f *recordingUsers) List(context.Context) ([]entity.User, error) {
	out := make([]entity.User, 0, len(f.m))
	for _, u := range f.m {
		out = append(out, u)
	}
	return out, nil
}

type recordingGrants struct {
	m       map[string]entity.Grant
	saved   []entity.Grant
	deleted []string
}

func (f *recordingGrants) key(email string, app entity.AppKey) string {
	return email + "|" + string(app)
}
func (f *recordingGrants) Find(_ context.Context, email string, app entity.AppKey) (*entity.Grant, error) {
	g, ok := f.m[f.key(email, app)]
	if !ok {
		return nil, nil
	}
	return &g, nil
}
func (f *recordingGrants) ListByEmail(_ context.Context, email string) ([]entity.Grant, error) {
	var out []entity.Grant
	for _, g := range f.m {
		if g.Email == email {
			out = append(out, g)
		}
	}
	return out, nil
}
func (f *recordingGrants) Save(_ context.Context, g entity.Grant) error {
	f.saved = append(f.saved, g)
	if f.m == nil {
		f.m = map[string]entity.Grant{}
	}
	f.m[f.key(g.Email, g.App)] = g
	return nil
}
func (f *recordingGrants) Delete(_ context.Context, email string, app entity.AppKey) error {
	f.deleted = append(f.deleted, f.key(email, app))
	delete(f.m, f.key(email, app))
	return nil
}

// fakeDirectory は Cognito の代わり。作成した email と、
// あらかじめ「存在する」ことにした email を持つ。
type fakeDirectory struct {
	existing map[string]entity.CognitoAccount
	created  []string
	enabled  map[string]bool
	// createErr が非 nil ならその error を返す（障害時の挙動を試す）。
	createErr error

	resent    []string
	resendErr error
	clients   []entity.AppClient
}

func (f *fakeDirectory) Find(_ context.Context, email string) (entity.CognitoAccount, error) {
	if acc, ok := f.existing[email]; ok {
		return acc, nil
	}
	return entity.CognitoAccount{Email: email, Status: entity.CognitoAbsent}, nil
}

func (f *fakeDirectory) Create(_ context.Context, email, _ string) error {
	if f.createErr != nil {
		return f.createErr
	}
	if _, ok := f.existing[email]; ok {
		return entity.ErrAlreadyExists
	}
	f.created = append(f.created, email)
	if f.existing == nil {
		f.existing = map[string]entity.CognitoAccount{}
	}
	f.existing[email] = entity.CognitoAccount{Email: email, Status: entity.CognitoInvited, Enabled: true}
	return nil
}

func (f *fakeDirectory) SetEnabled(_ context.Context, email string, enabled bool) error {
	if f.enabled == nil {
		f.enabled = map[string]bool{}
	}
	f.enabled[email] = enabled
	return nil
}

func (f *fakeDirectory) ListAppClients(_ context.Context) ([]entity.AppClient, error) {
	return f.clients, nil
}

func (f *fakeDirectory) ResendInvite(_ context.Context, email string) error {
	if f.resendErr != nil {
		return f.resendErr
	}
	f.resent = append(f.resent, email)
	return nil
}

func newConsole(u *recordingUsers, g *recordingGrants, i *fakeIdentities) *usecase.ConsoleUseCase {
	return newConsoleWithDir(u, g, i, &fakeDirectory{})
}

func newConsoleWithDir(
	u *recordingUsers, g *recordingGrants, i *fakeIdentities, d *fakeDirectory,
) *usecase.ConsoleUseCase {
	if u.m == nil {
		u.m = map[string]entity.User{}
	}
	if g.m == nil {
		g.m = map[string]entity.Grant{}
	}
	return usecase.NewConsoleUseCase(u, g, i, d, fixedClock{now: "2026-09-04T00:00:00Z"})
}

// --- SaveUser ------------------------------------------------------------

func TestSaveUser(t *testing.T) {
	t.Run("email を正規化して保存する", func(t *testing.T) {
		users := &recordingUsers{}
		uc := newConsole(users, &recordingGrants{}, &fakeIdentities{})

		got, err := uc.SaveUser(context.Background(), entity.User{
			Email: "  User@Example.COM ", Status: entity.StatusActive,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.Email != "user@example.com" {
			t.Errorf("Email = %q, want user@example.com", got.Email)
		}
	})

	t.Run("既存の CreatedAt を引き継ぐ", func(t *testing.T) {
		const me = "user@example.com"
		users := &recordingUsers{m: map[string]entity.User{
			me: {Email: me, Status: entity.StatusActive, CreatedAt: "2020-01-01T00:00:00Z"},
		}}
		uc := newConsole(users, &recordingGrants{}, &fakeIdentities{})

		got, err := uc.SaveUser(context.Background(), entity.User{
			Email: me, Status: entity.StatusSuspended,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.CreatedAt != "2020-01-01T00:00:00Z" {
			t.Errorf("CreatedAt = %q, 既存を引き継いでいない", got.CreatedAt)
		}
		if got.UpdatedAt != "2026-09-04T00:00:00Z" {
			t.Errorf("UpdatedAt = %q, 更新されていない", got.UpdatedAt)
		}
	})

	t.Run("email が空なら拒否", func(t *testing.T) {
		uc := newConsole(&recordingUsers{}, &recordingGrants{}, &fakeIdentities{})
		_, err := uc.SaveUser(context.Background(), entity.User{Status: entity.StatusActive})
		if !errors.Is(err, usecase.ErrInvalidInput) {
			t.Errorf("err = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("未知の status は拒否", func(t *testing.T) {
		uc := newConsole(&recordingUsers{}, &recordingGrants{}, &fakeIdentities{})
		_, err := uc.SaveUser(context.Background(), entity.User{
			Email: "a@b.com", Status: entity.UserStatus("bogus"),
		})
		if !errors.Is(err, usecase.ErrInvalidInput) {
			t.Errorf("err = %v, want ErrInvalidInput", err)
		}
	})
}

// --- DeleteUser ----------------------------------------------------------

func TestDeleteUser_Grantも消す(t *testing.T) {
	const me = "user@example.com"
	users := &recordingUsers{m: map[string]entity.User{me: {Email: me}}}
	grants := &recordingGrants{m: map[string]entity.Grant{
		me + "|task-scope":  {Email: me, App: entity.AppTaskScope},
		me + "|skilllogger": {Email: me, App: entity.AppSkillLogger},
	}}
	uc := newConsole(users, grants, &fakeIdentities{})

	if err := uc.DeleteUser(context.Background(), me); err != nil {
		t.Fatal(err)
	}

	// Grant が残ると「ユーザーは居ないのに許可だけある」状態になり、
	// 同じ email で再登録したとき権限が復活してしまう。
	if len(grants.m) != 0 {
		t.Errorf("Grant が %d 件残っている", len(grants.m))
	}
	if len(users.deleted) != 1 {
		t.Errorf("ユーザーが削除されていない")
	}
}

// --- SaveGrant -----------------------------------------------------------

func TestSaveGrant(t *testing.T) {
	const me = "user@example.com"

	t.Run("ユーザー未登録なら拒否", func(t *testing.T) {
		uc := newConsole(&recordingUsers{}, &recordingGrants{}, &fakeIdentities{})
		_, err := uc.SaveGrant(context.Background(), entity.Grant{
			Email: me, App: entity.AppTaskScope, Role: entity.RoleMember,
		})
		// 乗り入れ許可なしにアプリだけ使える状態を作らせない
		if !errors.Is(err, usecase.ErrInvalidInput) {
			t.Errorf("err = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("未知のアプリは拒否", func(t *testing.T) {
		users := &recordingUsers{m: map[string]entity.User{me: {Email: me}}}
		uc := newConsole(users, &recordingGrants{}, &fakeIdentities{})
		_, err := uc.SaveGrant(context.Background(), entity.Grant{
			Email: me, App: entity.AppKey("unknown-app"), Role: entity.RoleMember,
		})
		if !errors.Is(err, usecase.ErrInvalidInput) {
			t.Errorf("err = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("未知のロールは拒否", func(t *testing.T) {
		users := &recordingUsers{m: map[string]entity.User{me: {Email: me}}}
		uc := newConsole(users, &recordingGrants{}, &fakeIdentities{})
		_, err := uc.SaveGrant(context.Background(), entity.Grant{
			Email: me, App: entity.AppTaskScope, Role: entity.Role("root"),
		})
		if !errors.Is(err, usecase.ErrInvalidInput) {
			t.Errorf("err = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("Scopes が nil なら空配列にする", func(t *testing.T) {
		users := &recordingUsers{m: map[string]entity.User{me: {Email: me}}}
		uc := newConsole(users, &recordingGrants{}, &fakeIdentities{})

		got, err := uc.SaveGrant(context.Background(), entity.Grant{
			Email: me, App: entity.AppTaskScope, Role: entity.RoleMember, Scopes: nil,
		})
		if err != nil {
			t.Fatal(err)
		}
		// nil のまま保存すると DynamoDB で属性ごと消える。
		// 読み戻したとき「絞り込み無し」と区別がつかなくなるのを防ぐ。
		if got.Scopes == nil {
			t.Error("Scopes が nil のまま")
		}
	})

	t.Run("正常系", func(t *testing.T) {
		users := &recordingUsers{m: map[string]entity.User{me: {Email: me}}}
		grants := &recordingGrants{}
		uc := newConsole(users, grants, &fakeIdentities{})

		got, err := uc.SaveGrant(context.Background(), entity.Grant{
			Email: "  User@Example.COM ", App: entity.AppTaskScope,
			Role: entity.RoleMember, Scopes: []string{"seed-tech"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.Email != me {
			t.Errorf("Email = %q, 正規化されていない", got.Email)
		}
		if len(got.Scopes) != 1 || got.Scopes[0] != "seed-tech" {
			t.Errorf("Scopes = %v", got.Scopes)
		}
	})
}

// --- RecordSignIn --------------------------------------------------------

func TestRecordSignIn(t *testing.T) {
	ids := &fakeIdentities{}
	uc := newConsole(&recordingUsers{}, &recordingGrants{}, ids)

	if err := uc.RecordSignIn(context.Background(), "Google_123", " User@Example.COM ", "Google"); err != nil {
		t.Fatal(err)
	}
	if len(ids.saved) != 1 {
		t.Fatalf("記録件数 = %d, want 1", len(ids.saved))
	}
	if ids.saved[0].Email != "user@example.com" {
		t.Errorf("Email = %q, 正規化されていない", ids.saved[0].Email)
	}

	// sub が空のときは何も記録しない（キーが無いので保存できない）
	ids.saved = nil
	if err := uc.RecordSignIn(context.Background(), "", "a@b.com", "Google"); err != nil {
		t.Fatal(err)
	}
	if len(ids.saved) != 0 {
		t.Error("sub が空なのに記録している")
	}
}

// --- SaveUserWithCognito -------------------------------------------------

func TestSaveUserWithCognito(t *testing.T) {
	t.Run("createCognito が true なら Cognito にも作る", func(t *testing.T) {
		dir := &fakeDirectory{}
		uc := newConsoleWithDir(&recordingUsers{}, &recordingGrants{}, &fakeIdentities{}, dir)

		res, err := uc.SaveUserWithCognito(context.Background(), entity.User{
			Email: "New@Example.com", Status: entity.StatusActive,
		}, true)
		if err != nil {
			t.Fatalf("予期しないエラー: %v", err)
		}
		if !res.CognitoCreated {
			t.Error("CognitoCreated が false")
		}
		// 正規化された email で作られること（大文字のまま作ると
		// 台帳と Cognito で別レコードになる）
		if len(dir.created) != 1 || dir.created[0] != "new@example.com" {
			t.Errorf("作成された email が想定と違う: %v", dir.created)
		}
	})

	t.Run("createCognito が false なら Cognito を触らない", func(t *testing.T) {
		dir := &fakeDirectory{}
		uc := newConsoleWithDir(&recordingUsers{}, &recordingGrants{}, &fakeIdentities{}, dir)

		res, err := uc.SaveUserWithCognito(context.Background(), entity.User{
			Email: "a@example.com", Status: entity.StatusActive,
		}, false)
		if err != nil {
			t.Fatalf("予期しないエラー: %v", err)
		}
		if res.CognitoCreated || len(dir.created) != 0 {
			t.Error("作成しない指定なのに Cognito を作った（招待メールが飛ぶ）")
		}
	})

	t.Run("既に Cognito にいる場合はスキップして続行する", func(t *testing.T) {
		dir := &fakeDirectory{existing: map[string]entity.CognitoAccount{
			"dup@example.com": {Email: "dup@example.com", Status: entity.CognitoConfirmed},
		}}
		users := &recordingUsers{}
		uc := newConsoleWithDir(users, &recordingGrants{}, &fakeIdentities{}, dir)

		res, err := uc.SaveUserWithCognito(context.Background(), entity.User{
			Email: "dup@example.com", Status: entity.StatusActive,
		}, true)
		if err != nil {
			t.Fatalf("重複はエラーにしない方針だが失敗した: %v", err)
		}
		if !res.CognitoSkipped {
			t.Error("CognitoSkipped が false")
		}
		if len(users.saved) != 1 {
			t.Error("台帳への保存が行われていない")
		}
	})

	t.Run("Cognito 作成が失敗しても台帳は残る", func(t *testing.T) {
		// ここが肝。台帳を巻き戻すと、画面は「失敗」と出るのに
		// 実際は保存されている、という食い違いが起きる。
		dir := &fakeDirectory{createErr: errors.New("throttled")}
		users := &recordingUsers{}
		uc := newConsoleWithDir(users, &recordingGrants{}, &fakeIdentities{}, dir)

		res, err := uc.SaveUserWithCognito(context.Background(), entity.User{
			Email: "x@example.com", Status: entity.StatusActive,
		}, true)
		if err != nil {
			t.Fatalf("Cognito の失敗で全体を落としてはいけない: %v", err)
		}
		if res.CognitoError == "" {
			t.Error("CognitoError が空。失敗が画面に伝わらない")
		}
		if len(users.saved) != 1 {
			t.Error("台帳の保存が巻き戻された")
		}
	})
}

// --- CreateCognitoUser ---------------------------------------------------

func TestCreateCognitoUser(t *testing.T) {
	t.Run("台帳に無い人は作らない", func(t *testing.T) {
		// 層の飛び越し（ログインできるが台帳に無い）を作らせない。
		dir := &fakeDirectory{}
		uc := newConsoleWithDir(&recordingUsers{}, &recordingGrants{}, &fakeIdentities{}, dir)

		err := uc.CreateCognitoUser(context.Background(), "ghost@example.com")
		if !errors.Is(err, usecase.ErrInvalidInput) {
			t.Errorf("ErrInvalidInput を期待したが %v", err)
		}
		if len(dir.created) != 0 {
			t.Error("台帳に無いのに Cognito に作られた")
		}
	})

	t.Run("台帳にいる人は作れる", func(t *testing.T) {
		users := &recordingUsers{m: map[string]entity.User{
			"known@example.com": {Email: "known@example.com", Status: entity.StatusActive},
		}}
		dir := &fakeDirectory{}
		uc := newConsoleWithDir(users, &recordingGrants{}, &fakeIdentities{}, dir)

		if err := uc.CreateCognitoUser(context.Background(), "known@example.com"); err != nil {
			t.Fatalf("予期しないエラー: %v", err)
		}
		if len(dir.created) != 1 {
			t.Error("Cognito に作成されていない")
		}
	})
}

// --- CognitoStatusOf -----------------------------------------------------

func TestCognitoStatusOf(t *testing.T) {
	t.Run("台帳にあって Cognito に無い人を absent として返す", func(t *testing.T) {
		// この可視化が画面の主目的。ここが崩れると
		// 「登録したのにログインできない」に気付けなくなる。
		dir := &fakeDirectory{existing: map[string]entity.CognitoAccount{
			"there@example.com": {Email: "there@example.com", Status: entity.CognitoConfirmed},
		}}
		uc := newConsoleWithDir(&recordingUsers{}, &recordingGrants{}, &fakeIdentities{}, dir)

		got := uc.CognitoStatusOf(context.Background(),
			[]string{"there@example.com", "missing@example.com"})

		if got["there@example.com"].Status != entity.CognitoConfirmed {
			t.Errorf("既存の状態が違う: %v", got["there@example.com"].Status)
		}
		if got["missing@example.com"].Status != entity.CognitoAbsent {
			t.Errorf("未作成が absent になっていない: %v", got["missing@example.com"].Status)
		}
	})
}

// --- ResendInvite --------------------------------------------------------

func TestResendInvite(t *testing.T) {
	t.Run("台帳にいる人には再送できる", func(t *testing.T) {
		users := &recordingUsers{m: map[string]entity.User{
			"known@example.com": {Email: "known@example.com", Status: entity.StatusActive},
		}}
		dir := &fakeDirectory{}
		uc := newConsoleWithDir(users, &recordingGrants{}, &fakeIdentities{}, dir)

		if err := uc.ResendInvite(context.Background(), "Known@Example.com"); err != nil {
			t.Fatalf("予期しないエラー: %v", err)
		}
		// 正規化された email で送ること
		if len(dir.resent) != 1 || dir.resent[0] != "known@example.com" {
			t.Errorf("再送先が想定と違う: %v", dir.resent)
		}
	})

	t.Run("台帳に無い人には送らない", func(t *testing.T) {
		// コンソールの管理外にいる人へメールを飛ばす経路を作らない。
		dir := &fakeDirectory{}
		uc := newConsoleWithDir(&recordingUsers{}, &recordingGrants{}, &fakeIdentities{}, dir)

		err := uc.ResendInvite(context.Background(), "ghost@example.com")
		if !errors.Is(err, usecase.ErrInvalidInput) {
			t.Errorf("ErrInvalidInput を期待したが %v", err)
		}
		if len(dir.resent) != 0 {
			t.Error("台帳に無いのにメールが送られた")
		}
	})
}

// --- 招待の失効判定 ------------------------------------------------------

func TestInviteExpiry(t *testing.T) {
	cases := []struct {
		name        string
		acc         entity.CognitoAccount
		wantExpired bool
		wantLeft    int
		wantResend  bool
	}{
		{
			name: "招待中で期限内なら残日数を返す",
			acc: entity.CognitoAccount{
				Status: entity.CognitoInvited, InviteAgeDays: 5, InviteValidityDays: 7,
			},
			wantExpired: false, wantLeft: 2, wantResend: true,
		},
		{
			name: "期限ちょうどは失効とみなす",
			acc: entity.CognitoAccount{
				Status: entity.CognitoInvited, InviteAgeDays: 7, InviteValidityDays: 7,
			},
			wantExpired: true, wantLeft: 0, wantResend: true,
		},
		{
			name: "大幅に超過していても残日数は負にしない",
			acc: entity.CognitoAccount{
				Status: entity.CognitoInvited, InviteAgeDays: 41, InviteValidityDays: 7,
			},
			wantExpired: true, wantLeft: 0, wantResend: true,
		},
		{
			name: "有効期間が不明なら失効と断定しない",
			acc: entity.CognitoAccount{
				Status: entity.CognitoInvited, InviteAgeDays: 99, InviteValidityDays: 0,
			},
			wantExpired: false, wantLeft: 0, wantResend: true,
		},
		{
			name: "ログイン済みは失効の概念が無く再送もできない",
			acc: entity.CognitoAccount{
				Status: entity.CognitoConfirmed, InviteAgeDays: 99, InviteValidityDays: 7,
			},
			wantExpired: false, wantLeft: 0, wantResend: false,
		},
		{
			name:        "未作成は再送対象外",
			acc:         entity.CognitoAccount{Status: entity.CognitoAbsent},
			wantExpired: false, wantLeft: 0, wantResend: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.acc.InviteExpired(); got != c.wantExpired {
				t.Errorf("InviteExpired = %v, want %v", got, c.wantExpired)
			}
			if got := c.acc.InviteDaysLeft(); got != c.wantLeft {
				t.Errorf("InviteDaysLeft = %v, want %v", got, c.wantLeft)
			}
			if got := c.acc.CanResendInvite(); got != c.wantResend {
				t.Errorf("CanResendInvite = %v, want %v", got, c.wantResend)
			}
		})
	}
}

// --- Google 連携時の状態表示 ---------------------------------------------

// Google で使えている人を「失効」と出さないことを守る。
//
// 実際に起きた不具合の再発防止。Google 連携済みの人には
// ネイティブユーザーが FORCE_CHANGE_PASSWORD のまま残っており、
// そちらを見て状態を決めると「失効・要再送」と誤表示された。
func TestGoogleLinkedIsNotExpired(t *testing.T) {
	acc := entity.CognitoAccount{
		Status:       entity.CognitoConfirmed, // repo が Google 優先で確定させた結果
		GoogleLinked: true,
		Enabled:      true,
	}

	if acc.InviteExpired() {
		t.Error("Google で使えているのに失効と判定された")
	}
	if acc.CanResendInvite() {
		t.Error("Google 利用者に再送ボタンが出てしまう")
	}
}
