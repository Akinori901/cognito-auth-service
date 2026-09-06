package repo

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// LoadSecureParam は SSM Parameter Store から SecureString を読む。
//
// シークレットを Lambda の環境変数に直接置くと、コンソールや
// get-function-configuration で平文が見える。Parameter Store なら
// 値が設定に現れず、アクセス履歴も CloudTrail に残る。
//
// Secrets Manager ではなく Parameter Store を使うのは、標準パラメータが
// 無料で、今回は自動ローテーションが要らないため（両側の設定を同時に
// 変える必要があるので、自動で回されるとむしろ扱いにくい）。
//
// 起動時に 1 回だけ呼ぶ想定。呼ぶたびに API を叩くので、
// リクエストごとに呼んではいけない。
func LoadSecureParam(ctx context.Context, cfg aws.Config, name string) (string, error) {
	if name == "" {
		return "", nil
	}

	out, err := ssm.NewFromConfig(cfg).GetParameter(ctx, &ssm.GetParameterInput{
		Name:           aws.String(name),
		WithDecryption: aws.Bool(true),
	})
	if err != nil {
		return "", fmt.Errorf("パラメータ %s の取得に失敗: %w", name, err)
	}
	if out.Parameter == nil || out.Parameter.Value == nil {
		return "", fmt.Errorf("パラメータ %s が空です", name)
	}
	return *out.Parameter.Value, nil
}
