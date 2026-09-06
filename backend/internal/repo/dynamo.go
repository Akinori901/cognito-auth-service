// Package repo は外部技術（DynamoDB / Cognito）との接続を担う。
//
// **この層は usecase を import しない。** 契約は usecase 側の interface で
// 定義され、ここはそれを「満たす」だけ（依存性逆転）。import すると
// 依存が外から内へ逆流し、usecase のテストに DynamoDB が要るようになる。
package repo

import (
	"context"
	"fmt"

	"github.com/Akinori901/cognito-auth-service/backend/internal/entity"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// DynamoConfig はテーブル名の設定。
//
// テーブル名を環境変数から渡すのは、ローカル検証で別テーブルに向けられるようにするため。
type DynamoConfig struct {
	UsersTable      string
	GrantsTable     string
	IdentitiesTable string
}

// --- ユーザー台帳 ---------------------------------------------------------

// UserRepo は usecase.UserRepository の DynamoDB 実装。
type UserRepo struct {
	db    *dynamodb.Client
	table string
}

func NewUserRepo(db *dynamodb.Client, table string) *UserRepo {
	return &UserRepo{db: db, table: table}
}

// dynamoUser は DynamoDB の項目表現。
//
// entity.User をそのまま使わないのは、entity に dynamodbav タグを付けると
// entity が DynamoDB を知ることになるため（層の規約違反）。
type dynamoUser struct {
	Email          string `dynamodbav:"email"`
	DisplayName    string `dynamodbav:"displayName"`
	Status         string `dynamodbav:"status"`
	IsConsoleAdmin bool   `dynamodbav:"isConsoleAdmin"`
	Note           string `dynamodbav:"note"`
	CreatedAt      string `dynamodbav:"createdAt"`
	UpdatedAt      string `dynamodbav:"updatedAt"`
}

func (d dynamoUser) toEntity() entity.User {
	return entity.User{
		Email:          d.Email,
		DisplayName:    d.DisplayName,
		Status:         entity.UserStatus(d.Status),
		IsConsoleAdmin: d.IsConsoleAdmin,
		Note:           d.Note,
		CreatedAt:      d.CreatedAt,
		UpdatedAt:      d.UpdatedAt,
	}
}

func fromEntityUser(u entity.User) dynamoUser {
	return dynamoUser{
		Email:          u.Email,
		DisplayName:    u.DisplayName,
		Status:         string(u.Status),
		IsConsoleAdmin: u.IsConsoleAdmin,
		Note:           u.Note,
		CreatedAt:      u.CreatedAt,
		UpdatedAt:      u.UpdatedAt,
	}
}

// FindByEmail は email でユーザーを引く。見つからなければ (nil, nil)。
//
// 「見つからない」をエラーにしないのは、認可判定において未登録が正常な入力だから。
// エラーと区別できないと、DynamoDB の障害と未登録を取り違える。
func (r *UserRepo) FindByEmail(ctx context.Context, email string) (*entity.User, error) {
	out, err := r.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &r.table,
		Key: map[string]types.AttributeValue{
			"email": &types.AttributeValueMemberS{Value: email},
		},
		// 認可判定は書き込み直後に走りうる（コンソールで許可した直後にログイン）。
		// 結果整合だと「設定したのに入れない」が起きるため強整合で読む。
		ConsistentRead: boolPtr(true),
	})
	if err != nil {
		return nil, fmt.Errorf("users の取得に失敗: %w", err)
	}
	if out.Item == nil {
		return nil, nil
	}
	var d dynamoUser
	if err := attributevalue.UnmarshalMap(out.Item, &d); err != nil {
		return nil, fmt.Errorf("users の変換に失敗: %w", err)
	}
	u := d.toEntity()
	return &u, nil
}

func (r *UserRepo) Save(ctx context.Context, u entity.User) error {
	item, err := attributevalue.MarshalMap(fromEntityUser(u))
	if err != nil {
		return fmt.Errorf("users の変換に失敗: %w", err)
	}
	if _, err := r.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &r.table,
		Item:      item,
	}); err != nil {
		return fmt.Errorf("users の保存に失敗: %w", err)
	}
	return nil
}

func (r *UserRepo) Delete(ctx context.Context, email string) error {
	if _, err := r.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: &r.table,
		Key: map[string]types.AttributeValue{
			"email": &types.AttributeValueMemberS{Value: email},
		},
	}); err != nil {
		return fmt.Errorf("users の削除に失敗: %w", err)
	}
	return nil
}

// List は全ユーザーを返す。コンソールの一覧表示用。
//
// Scan を使うのは、この表が「共通認証基盤の利用者」だけを持つ小さな表だから。
// 数千件に育つ想定なら GSI で分割する必要があるが、現状その規模にはならない。
func (r *UserRepo) List(ctx context.Context) ([]entity.User, error) {
	var users []entity.User
	var start map[string]types.AttributeValue
	for {
		out, err := r.db.Scan(ctx, &dynamodb.ScanInput{
			TableName:         &r.table,
			ExclusiveStartKey: start,
		})
		if err != nil {
			return nil, fmt.Errorf("users の一覧取得に失敗: %w", err)
		}
		var ds []dynamoUser
		if err := attributevalue.UnmarshalListOfMaps(out.Items, &ds); err != nil {
			return nil, fmt.Errorf("users の変換に失敗: %w", err)
		}
		for _, d := range ds {
			users = append(users, d.toEntity())
		}
		if out.LastEvaluatedKey == nil {
			break
		}
		start = out.LastEvaluatedKey
	}
	return users, nil
}

// --- 許可（アプリ利用可否とスコープ）--------------------------------------

// GrantRepo は usecase.GrantRepository の DynamoDB 実装。
type GrantRepo struct {
	db    *dynamodb.Client
	table string
}

func NewGrantRepo(db *dynamodb.Client, table string) *GrantRepo {
	return &GrantRepo{db: db, table: table}
}

type dynamoGrant struct {
	Email     string   `dynamodbav:"email"`
	App       string   `dynamodbav:"app"`
	Role      string   `dynamodbav:"role"`
	Scopes    []string `dynamodbav:"scopes"`
	CreatedAt string   `dynamodbav:"createdAt"`
	UpdatedAt string   `dynamodbav:"updatedAt"`
}

func (d dynamoGrant) toEntity() entity.Grant {
	return entity.Grant{
		Email:     d.Email,
		App:       entity.AppKey(d.App),
		Role:      entity.Role(d.Role),
		Scopes:    d.Scopes,
		CreatedAt: d.CreatedAt,
		UpdatedAt: d.UpdatedAt,
	}
}

func (r *GrantRepo) Find(ctx context.Context, email string, app entity.AppKey) (*entity.Grant, error) {
	out, err := r.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &r.table,
		Key: map[string]types.AttributeValue{
			"email": &types.AttributeValueMemberS{Value: email},
			"app":   &types.AttributeValueMemberS{Value: string(app)},
		},
		ConsistentRead: boolPtr(true),
	})
	if err != nil {
		return nil, fmt.Errorf("grants の取得に失敗: %w", err)
	}
	if out.Item == nil {
		return nil, nil
	}
	var d dynamoGrant
	if err := attributevalue.UnmarshalMap(out.Item, &d); err != nil {
		return nil, fmt.Errorf("grants の変換に失敗: %w", err)
	}
	g := d.toEntity()
	return &g, nil
}

func (r *GrantRepo) ListByEmail(ctx context.Context, email string) ([]entity.Grant, error) {
	out, err := r.db.Query(ctx, &dynamodb.QueryInput{
		TableName:              &r.table,
		KeyConditionExpression: strPtr("email = :e"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":e": &types.AttributeValueMemberS{Value: email},
		},
		ConsistentRead: boolPtr(true),
	})
	if err != nil {
		return nil, fmt.Errorf("grants の一覧取得に失敗: %w", err)
	}
	var ds []dynamoGrant
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &ds); err != nil {
		return nil, fmt.Errorf("grants の変換に失敗: %w", err)
	}
	grants := make([]entity.Grant, 0, len(ds))
	for _, d := range ds {
		grants = append(grants, d.toEntity())
	}
	return grants, nil
}

func (r *GrantRepo) Save(ctx context.Context, g entity.Grant) error {
	item, err := attributevalue.MarshalMap(dynamoGrant{
		Email:     g.Email,
		App:       string(g.App),
		Role:      string(g.Role),
		Scopes:    g.Scopes,
		CreatedAt: g.CreatedAt,
		UpdatedAt: g.UpdatedAt,
	})
	if err != nil {
		return fmt.Errorf("grants の変換に失敗: %w", err)
	}
	if _, err := r.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &r.table,
		Item:      item,
	}); err != nil {
		return fmt.Errorf("grants の保存に失敗: %w", err)
	}
	return nil
}

func (r *GrantRepo) Delete(ctx context.Context, email string, app entity.AppKey) error {
	if _, err := r.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: &r.table,
		Key: map[string]types.AttributeValue{
			"email": &types.AttributeValueMemberS{Value: email},
			"app":   &types.AttributeValueMemberS{Value: string(app)},
		},
	}); err != nil {
		return fmt.Errorf("grants の削除に失敗: %w", err)
	}
	return nil
}

// --- ログイン経路の記録 ---------------------------------------------------

// IdentityRepo は usecase.IdentityRepository の DynamoDB 実装。
type IdentityRepo struct {
	db    *dynamodb.Client
	table string
}

func NewIdentityRepo(db *dynamodb.Client, table string) *IdentityRepo {
	return &IdentityRepo{db: db, table: table}
}

type dynamoIdentity struct {
	Sub            string `dynamodbav:"sub"`
	Email          string `dynamodbav:"email"`
	Provider       string `dynamodbav:"provider"`
	LastSignedInAt string `dynamodbav:"lastSignedInAt"`
}

// Upsert はログインのたびに sub → email を記録する。
//
// email をキーにする設計の弱点は「email が変わると設定が孤立する」こと。
// ここに残しておけば、変更後も sub から辿れる。
func (r *IdentityRepo) Upsert(ctx context.Context, i entity.Identity) error {
	item, err := attributevalue.MarshalMap(dynamoIdentity{
		Sub:            i.Sub,
		Email:          i.Email,
		Provider:       i.Provider,
		LastSignedInAt: i.LastSignedInAt,
	})
	if err != nil {
		return fmt.Errorf("identities の変換に失敗: %w", err)
	}
	if _, err := r.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: &r.table,
		Item:      item,
	}); err != nil {
		return fmt.Errorf("identities の保存に失敗: %w", err)
	}
	return nil
}

func (r *IdentityRepo) ListByEmail(ctx context.Context, email string) ([]entity.Identity, error) {
	out, err := r.db.Query(ctx, &dynamodb.QueryInput{
		TableName:              &r.table,
		IndexName:              strPtr("byEmail"),
		KeyConditionExpression: strPtr("email = :e"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":e": &types.AttributeValueMemberS{Value: email},
		},
		// GSI は強整合read をサポートしない。ここは履歴表示用なので結果整合で足りる。
	})
	if err != nil {
		return nil, fmt.Errorf("identities の一覧取得に失敗: %w", err)
	}
	var ds []dynamoIdentity
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &ds); err != nil {
		return nil, fmt.Errorf("identities の変換に失敗: %w", err)
	}
	ids := make([]entity.Identity, 0, len(ds))
	for _, d := range ds {
		ids = append(ids, entity.Identity{
			Sub:            d.Sub,
			Email:          d.Email,
			Provider:       d.Provider,
			LastSignedInAt: d.LastSignedInAt,
		})
	}
	return ids, nil
}

func boolPtr(b bool) *bool    { return &b }
func strPtr(s string) *string { return &s }
