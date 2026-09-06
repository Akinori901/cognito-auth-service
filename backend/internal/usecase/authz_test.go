package usecase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Akinori901/cognito-auth-service/backend/internal/entity"
	"github.com/Akinori901/cognito-auth-service/backend/internal/usecase"
)

// --- テスト用のフェイク ---------------------------------------------------
//
// DynamoDB を立てずに判定ロジックだけを検証する。
// usecase が interface に依存しているからこれができる（依存性逆転の効能）。

type fakeUsers struct {
	m   map[string]entity.User
	err error
}

func (f *fakeUsers) FindByEmail(_ context.Context, email string) (*entity.User, error) {
	if f.err != nil {
		return nil, f.err
	}
	u, ok := f.m[email]
	if !ok {
		return nil, nil
	}
	return &u, nil
}
func (f *fakeUsers) Save(context.Context, entity.User) error        { return nil }
func (f *fakeUsers) Delete(context.Context, string) error           { return nil }
func (f *fakeUsers) List(context.Context) ([]entity.User, error)    { return nil, nil }

type fakeGrants struct {
	m   map[string]entity.Grant // key: email + "|" + app
	err error
}

func (f *fakeGrants) Find(_ context.Context, email string, app entity.AppKey) (*entity.Grant, error) {
	if f.err != nil {
		return nil, f.err
	}
	g, ok := f.m[email+"|"+string(app)]
	if !ok {
		return nil, nil
	}
	return &g, nil
}
func (f *fakeGrants) ListByEmail(context.Context, string) ([]entity.Grant, error) { return nil, nil }
func (f *fakeGrants) Save(context.Context, entity.Grant) error                    { return nil }
func (f *fakeGrants) Delete(context.Context, string, entity.AppKey) error         { return nil }

// --- Authorize -----------------------------------------------------------

func TestAuthorize(t *testing.T) {
	const me = "user@example.com"

	active := entity.User{Email: me, Status: entity.StatusActive}
	grant := entity.Grant{
		Email:  me,
		App:    entity.AppTaskScope,
		Role:   entity.RoleMember,
		Scopes: []string{"seed-tech"},
	}

	tests := []struct {
		name    string
		users   *fakeUsers
		grants  *fakeGrants
		email   string
		app     entity.AppKey
		want    bool
		wantErr bool
	}{
		{
			name:   "登録済み・有効・Grant あり → 許可",
			users:  &fakeUsers{m: map[string]entity.User{me: active}},
			grants: &fakeGrants{m: map[string]entity.Grant{me + "|task-scope": grant}},
			email:  me,
			app:    entity.AppTaskScope,
			want:   true,
		},
		{
			name:   "大文字で来ても同じ人として扱う",
			users:  &fakeUsers{m: map[string]entity.User{me: active}},
			grants: &fakeGrants{m: map[string]entity.Grant{me + "|task-scope": grant}},
			email:  "User@Example.COM",
			app:    entity.AppTaskScope,
			want:   true,
		},
		{
			name:   "前後の空白を無視する",
			users:  &fakeUsers{m: map[string]entity.User{me: active}},
			grants: &fakeGrants{m: map[string]entity.Grant{me + "|task-scope": grant}},
			email:  "  user@example.com  ",
			app:    entity.AppTaskScope,
			want:   true,
		},
		{
			name:   "email が空 → 拒否",
			users:  &fakeUsers{m: map[string]entity.User{me: active}},
			grants: &fakeGrants{m: map[string]entity.Grant{me + "|task-scope": grant}},
			email:  "",
			app:    entity.AppTaskScope,
			want:   false,
		},
		{
			name:   "ユーザー未登録 → 拒否",
			users:  &fakeUsers{m: map[string]entity.User{}},
			grants: &fakeGrants{m: map[string]entity.Grant{me + "|task-scope": grant}},
			email:  me,
			app:    entity.AppTaskScope,
			want:   false,
		},
		{
			// Grant があっても停止中なら通さない。退職者の締め出しはここで効く。
			name: "停止中のユーザーは Grant があっても拒否",
			users: &fakeUsers{m: map[string]entity.User{
				me: {Email: me, Status: entity.StatusSuspended},
			}},
			grants: &fakeGrants{m: map[string]entity.Grant{me + "|task-scope": grant}},
			email:  me,
			app:    entity.AppTaskScope,
			want:   false,
		},
		{
			name:   "Grant が無い → 拒否",
			users:  &fakeUsers{m: map[string]entity.User{me: active}},
			grants: &fakeGrants{m: map[string]entity.Grant{}},
			email:  me,
			app:    entity.AppTaskScope,
			want:   false,
		},
		{
			// task-scope の Grant で skilllogger には入れない
			name:   "別アプリの Grant では通さない",
			users:  &fakeUsers{m: map[string]entity.User{me: active}},
			grants: &fakeGrants{m: map[string]entity.Grant{me + "|task-scope": grant}},
			email:  me,
			app:    entity.AppSkillLogger,
			want:   false,
		},
		{
			// DynamoDB 障害時に「判定できないから通す」が事故。必ず拒否側へ。
			name:    "users の取得に失敗 → 拒否 + エラー",
			users:   &fakeUsers{err: errors.New("dynamodb down")},
			grants:  &fakeGrants{m: map[string]entity.Grant{}},
			email:   me,
			app:     entity.AppTaskScope,
			want:    false,
			wantErr: true,
		},
		{
			name:    "grants の取得に失敗 → 拒否 + エラー",
			users:   &fakeUsers{m: map[string]entity.User{me: active}},
			grants:  &fakeGrants{err: errors.New("dynamodb down")},
			email:   me,
			app:     entity.AppTaskScope,
			want:    false,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := usecase.NewAuthzUseCase(tt.users, tt.grants)
			got, err := uc.Authorize(context.Background(), tt.email, tt.app)

			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if got.Allowed != tt.want {
				t.Errorf("Allowed = %v, want %v", got.Allowed, tt.want)
			}
			// 拒否のときは role/scopes を漏らさない
			if !tt.want && (got.Role != "" || len(got.Scopes) > 0) {
				t.Errorf("拒否なのに role=%q scopes=%v が返っている", got.Role, got.Scopes)
			}
		})
	}
}

func TestAuthorize_許可時はスコープを返す(t *testing.T) {
	const me = "user@example.com"
	uc := usecase.NewAuthzUseCase(
		&fakeUsers{m: map[string]entity.User{me: {Email: me, Status: entity.StatusActive}}},
		&fakeGrants{m: map[string]entity.Grant{
			me + "|task-scope": {
				Email: me, App: entity.AppTaskScope,
				Role: entity.RoleMember, Scopes: []string{"seed-tech"},
			},
		}},
	)

	got, err := uc.Authorize(context.Background(), me, entity.AppTaskScope)
	if err != nil {
		t.Fatal(err)
	}
	if got.Role != entity.RoleMember {
		t.Errorf("Role = %q, want member", got.Role)
	}
	if len(got.Scopes) != 1 || got.Scopes[0] != "seed-tech" {
		t.Errorf("Scopes = %v, want [seed-tech]", got.Scopes)
	}
}

// --- CanUseConsole -------------------------------------------------------

func TestCanUseConsole(t *testing.T) {
	const admin = "admin@example.com"
	const other = "other@example.com"

	users := &fakeUsers{m: map[string]entity.User{
		admin: {Email: admin, Status: entity.StatusActive, IsConsoleAdmin: true},
		other: {Email: other, Status: entity.StatusActive, IsConsoleAdmin: false},
	}}

	tests := []struct {
		name       string
		email      string
		provider   string
		breakGlass string
		want       bool
	}{
		{"Google + コンソール管理者 → 可", admin, entity.ProviderGoogle, "", true},
		{"Cognito 直接ログインは不可", admin, entity.ProviderCognito, "", false},
		{"管理者フラグが無ければ不可", other, entity.ProviderGoogle, "", false},
		{"未登録ユーザーは不可", "nobody@example.com", entity.ProviderGoogle, "", false},
		{"email が空 → 不可", "", entity.ProviderGoogle, "", false},

		// 緊急用の抜け道。Google IdP が死んだときの復旧手段。
		{"break-glass 指定なら Cognito でも可", admin, entity.ProviderCognito, admin, true},
		{"break-glass は指定した email にだけ効く", other, entity.ProviderCognito, admin, false},
		{"break-glass が空なら誰にも効かない", admin, entity.ProviderCognito, "", false},
		{
			// break-glass でも「コンソール管理者であること」は外さない
			"break-glass でも管理者フラグは必要", other, entity.ProviderCognito, other, false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uc := usecase.NewAuthzUseCase(users, &fakeGrants{m: map[string]entity.Grant{}})
			got, err := uc.CanUseConsole(context.Background(), tt.email, tt.provider, tt.breakGlass)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("CanUseConsole = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCanUseConsole_停止中は入れない(t *testing.T) {
	const me = "admin@example.com"
	users := &fakeUsers{m: map[string]entity.User{
		me: {Email: me, Status: entity.StatusSuspended, IsConsoleAdmin: true},
	}}
	uc := usecase.NewAuthzUseCase(users, &fakeGrants{m: map[string]entity.Grant{}})

	got, err := uc.CanUseConsole(context.Background(), me, entity.ProviderGoogle, "")
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Error("停止中のユーザーがコンソールに入れてしまう")
	}
}
