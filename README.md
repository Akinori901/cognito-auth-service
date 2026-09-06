# cognito-auth-service

**複数のアプリで 1 つのログインを共有するための、Cognito ベースの認証基盤と管理コンソール。**

アプリごとに User Pool を立てると、ユーザーはアプリの数だけアカウントを作ることになる。
このリポジトリは User Pool を 1 つに集約し、アプリは **App Client 単位**で相乗りする構成を、
Terraform（インフラ）＋ Go（API）＋ React（管理コンソール）の一式で提供する。

<p>
  <img alt="Go" src="https://img.shields.io/badge/Go-1.23-00ADD8?logo=go&logoColor=white">
  <img alt="React" src="https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black">
  <img alt="TypeScript" src="https://img.shields.io/badge/TypeScript-5-3178C6?logo=typescript&logoColor=white">
  <img alt="Terraform" src="https://img.shields.io/badge/Terraform-AWS-844FBA?logo=terraform&logoColor=white">
  <img alt="Cognito" src="https://img.shields.io/badge/Amazon_Cognito-DD344C?logo=amazonwebservices&logoColor=white">
  <img alt="License" src="https://img.shields.io/badge/License-MIT-green">
</p>

> 本リポジトリは非公開の開発リポジトリから、リリース時点のスナップショットを公開しているミラーです
> （コミット履歴はリリース単位）。本番の識別子はプレースホルダに置き換えてあります。

## なぜ作ったか

個人で複数のサービスを運用していると、認証が各アプリに散らばる。
アプリを 1 つ足すたびに User Pool を作り、ログイン画面を作り、招待の仕組みを作る羽目になる。
しかも「誰がどのアプリを使ってよいか」はどこにも書かれていない。

そこで **認証（ログインできるか）と認可（どのアプリを使ってよいか）を分けて**、
前者を Cognito に、後者を DynamoDB の台帳に持たせた。
アプリ側は JWT を検証するだけでよく、ユーザー管理のコードを持たなくて済む。

## 3 つの関門

ログインには 3 段階あり、**すべてを通らないとアプリを使えない**。

| 関門 | 実体 | 意味 |
|------|------|------|
| 1. 認証 | Cognito User Pool | そもそもログインできるか |
| 2. 台帳 | DynamoDB `users` | この基盤に乗り入れてよい人か |
| 3. 許可 | DynamoDB `grants` | どのアプリを・どの範囲まで使えるか |

Google ログインを使う場合も 1 は必要になる。このプールは Pre-Sign-Up トリガー
（`terraform/lambda/presignup/index.py`）で**事前に登録されていないアカウントを拒否する**。
招待した人だけが入れる、閉じた運用を前提にしている。

## 構成

| 層 | 実体 | 役割 |
|---|---|---|
| インフラ | Terraform | User Pool / Hosted UI / Google IdP / DynamoDB / Lambda / API Gateway / CloudFront |
| API | Go（クリーンアーキテクチャ） | 台帳と許可の CRUD、Cognito 操作 |
| コンソール | React + TypeScript | ユーザー一覧・登録・招待・アプリ別の権限付与 |

### Go の層構成は CI で機械検証する

```
cmd/ → internal/app/ → internal/controller/ → internal/usecase/ → internal/repo/
                                                     ↑
                                              internal/entity/
```

依存の向きは [`backend/.go-arch-lint.yml`](backend/.go-arch-lint.yml) に落としてあり、
`go-arch-lint check` で違反を検出する。**規約を文章で配るだけでは守られない**ので、
CI で落ちる形にしている。

```bash
go install github.com/fe3dback/go-arch-lint@latest
cd backend && go-arch-lint check
```

## セットアップ

### 1. Google OAuth クライアントを作る

Google Cloud Console でウェブアプリケーションとして作成し、承認済みリダイレクト URI に
apply 後の `terraform output google_redirect_uri` の値を設定する
（`https://<domain_prefix>.auth.<region>.amazoncognito.com/oauth2/idpresponse`）。

Google ログインを使わない場合はこの手順を飛ばし、`terraform.tfvars` の Google 関連を空にする。

### 2. Terraform を適用する

```bash
cp terraform/terraform.tfvars.example terraform/terraform.tfvars
# AWS アカウント ID / callback URL / Google 認証情報 / 初期管理者メールを設定

cd terraform
terraform init
terraform plan     # 作成内容を確認
terraform apply
```

`initial_admin_email` に指定したアドレスは初期管理者として登録される。
**自分を締め出さないための保険**なので必ず設定すること。

### 3. アプリ側に値を渡す

`terraform output` の値を各アプリの設定に入れる。

| 用途 | output | アプリ側の設定名（例） |
|---|---|---|
| プール ID | `user_pool_id` | `COGNITO_USER_POOL_ID` |
| App Client ID | `<app>_web_client_id` | `COGNITO_WEB_CLIENT_ID` |
| Hosted UI ドメイン | `domain_prefix` | `COGNITO_DOMAIN_PREFIX` |

### 4. ローカルで動かす

```bash
docker compose up
```

## アプリを追加する

1. `terraform/cognito.tf` に App Client を追加して apply
2. `terraform output` の client id をそのアプリに設定
3. コンソールの「アプリ」一覧に追加（`frontend/src/api/client.ts` の `APPS`）
4. ユーザー詳細画面で、そのアプリの利用を許可する

アプリ側に必要なのは **JWT の検証だけ**。ユーザーの作成・無効化・招待の再送は
すべてこのコンソールが行うので、各アプリがユーザー管理画面を持つ必要はない。

## 締め出し防止

- User Pool は `deletion_protection = ACTIVE`
- 初期管理者は Terraform で作られるので、コンソールが壊れても AWS コンソールから入れる
- 台帳（DynamoDB）が消えても Cognito のユーザーは残る。逆も同じ

## ライセンス

[MIT](LICENSE)
