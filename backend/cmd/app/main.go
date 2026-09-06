// Command app は認証コンソールのバックエンド。
//
// ローカルでは HTTP サーバーとして、AWS では Lambda として動く。
// 切り替えは AWS_LAMBDA_FUNCTION_NAME の有無で判定する（Lambda が必ず設定する）。
// 同じバイナリで両方動かせるので、ローカルで確かめたものがそのまま本番へ行く。
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Akinori901/cognito-auth-service/backend/internal/app"
	"github.com/aws/aws-lambda-go/lambda"
	chiadapter "github.com/awslabs/aws-lambda-go-api-proxy/chi"
)

func main() {
	ctx := context.Background()

	cfg, err := app.LoadConfig()
	if err != nil {
		// 設定漏れのまま動くと「なぜか認証が通らない」状態になる。
		// 起動時に落として気づけるようにする。
		log.Fatalf("設定の読み込みに失敗しました: %v", err)
	}

	handler, err := app.NewHandler(ctx, cfg)
	if err != nil {
		log.Fatalf("初期化に失敗しました: %v", err)
	}

	// API Gateway HTTP API (payload v2) 経由で動かす。
	if os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != "" {
		lambda.Start(chiadapter.NewV2(handler).ProxyWithContextV2)
		return
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("listening on :%s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("サーバーが停止しました: %v", err)
	}
}
