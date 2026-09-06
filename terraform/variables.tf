variable "aws_region" {
  description = "デプロイ先リージョン"
  type        = string
  default     = "ap-northeast-1"
}

variable "allowed_account_ids" {
  description = "apply を許可する AWS アカウントID（誤アカウント防止ガード）"
  type        = list(string)
}

# --- Cognito 命名 ---

variable "pool_name" {
  description = "Cognito User Pool 名（中立名）"
  type        = string
  default     = "auth-user-pool"
}

variable "domain_prefix" {
  description = "Cognito Hosted UI ドメインの prefix（{prefix}.auth.{region}.amazoncognito.com）"
  type        = string
  default     = "auth"
}

# --- 収容アプリ（App Client）の callback / logout URL ---
# アプリを追加するたびに、そのアプリの URL をここに足す（複数アプリ集約の共通基盤）。

variable "skilllogger_callback_urls" {
  description = "SkillLogger の OAuth callback URL 一覧（ローカル + 本番 CloudFront）"
  type        = list(string)
}

variable "skilllogger_logout_urls" {
  description = "SkillLogger の サインアウト後リダイレクト URL 一覧"
  type        = list(string)
}

variable "money_pilot_callback_urls" {
  description = "MoneyPilot の OAuth callback URL 一覧（ローカル + 本番 CloudFront）"
  type        = list(string)
}

variable "money_pilot_logout_urls" {
  description = "MoneyPilot の サインアウト後リダイレクト URL 一覧"
  type        = list(string)
}

variable "task_scope_callback_urls" {
  description = "task-scope の OAuth callback URL 一覧（ローカル + 本番 CloudFront）"
  type        = list(string)
}

variable "task_scope_logout_urls" {
  description = "task-scope の サインアウト後リダイレクト URL 一覧"
  type        = list(string)
}

# --- Google IdP（Google Cloud Console で取得した OAuth クライアント情報）---
# 承認済みリダイレクト URI:
#   https://{domain_prefix}.auth.{region}.amazoncognito.com/oauth2/idpresponse

variable "google_oauth_client_id" {
  description = "Google OAuth 2.0 クライアントID（xxx.apps.googleusercontent.com）"
  type        = string
  sensitive   = true
}

variable "google_oauth_client_secret" {
  description = "Google OAuth 2.0 クライアントシークレット（GOCSPX-xxx）"
  type        = string
  sensitive   = true
}

# --- 初期ユーザー（締め出し防止の初期アカウント）---
# apply 時に Cognito にこのメールで管理者ユーザーを作成し、
# アプリ側 DB の allowed_email 初期レコードもこのメールで登録する（backend PR 側）。

variable "auth_console_callback_urls" {
  description = "認証コンソールの OAuth callback URL 一覧（ローカル + 本番 CloudFront）"
  type        = list(string)
  default     = ["http://localhost:5173"]
}

variable "auth_console_logout_urls" {
  description = "認証コンソールの サインアウト後リダイレクト URL 一覧"
  type        = list(string)
  default     = ["http://localhost:5173"]
}

variable "initial_admin_email" {
  description = "初期登録するアカウントのメールアドレス（Google ログインするアドレス）"
  type        = string
  sensitive   = true
}

variable "lambda_artifact_key" {
  description = "Lambda のビルド成果物の S3 キー。CD が更新するので、初回 apply 用の既定値のみ持つ"
  type        = string
  default     = "bootstrap.zip"
}

variable "break_glass_email" {
  description = "緊急用の抜け道。Google IdP が使えなくなったときだけ設定する。通常は空"
  type        = string
  default     = ""
}

variable "console_secret_param" {
  description = "共有シークレットを置いた SSM パラメータ名。値は Terraform で管理しない（tfstate に平文で残るため手で作る）"
  type        = string
  default     = "/auth-console/shared-secret"
}

variable "scope_endpoint_task_scope" {
  description = "task-scope のスコープ候補エンドポイント。空なら候補なしになる"
  type        = string
  default     = ""
}

variable "scope_endpoint_money_pilot" {
  description = "money-pilot のスコープ候補エンドポイント。空なら候補なしになる"
  type        = string
  default     = ""
}

# -----------------------------------------------------------------------------
# 招待メールの送信元（SES）
# -----------------------------------------------------------------------------
#
# **空にしておくと Cognito の既定送信のまま動く。** 公開版を試す人が
# ドメインを持っていなくても apply できるよう、任意設定にしてある。
#
# 既定送信は 1 日 50 通の上限があり、送信元が AWS 利用者の共用ドメイン
# （no-reply@verificationemail.com）のため迷惑メール判定されやすい。
# 社外の人を招待するなら SES に切り替える。

variable "ses_domain" {
  description = "招待メールの送信元ドメイン（例: example.com）。空なら Cognito 既定送信のまま"
  type        = string
  default     = ""
}

variable "ses_from_local_part" {
  description = "送信元のローカル部。ses_domain と組んで no-reply@example.com のようになる"
  type        = string
  default     = "no-reply"
}

variable "ses_from_display_name" {
  description = "受信者のメーラーに出る差出人名"
  type        = string
  default     = "Auth"
}

variable "ses_manage_dns" {
  description = <<-EOT
    DKIM の CNAME を Terraform で作るか。

    true  : Route53 に該当ゾーンがある場合。ses_route53_zone_id も指定する
    false : DNS が外部（ヘテムル等）にある場合。出力された値を手で登録する
  EOT
  type        = bool
  default     = false
}

variable "ses_route53_zone_id" {
  description = "ses_manage_dns = true のときに DKIM レコードを作る Route53 ゾーン ID"
  type        = string
  default     = ""
}

# -----------------------------------------------------------------------------
# fair-value-calculator（統合先として受け入れる）
# -----------------------------------------------------------------------------

variable "fvc_callback_urls" {
  description = "FVC Web の callback URL"
  type        = list(string)
  default = [
    "http://localhost:8888/auth/callback",
    "https://example-app-5.cloudfront.net/auth/callback",
  ]
}

variable "fvc_logout_urls" {
  description = "FVC Web の logout URL"
  type        = list(string)
  default = [
    "http://localhost:8888/login",
    "https://example-app-5.cloudfront.net/login",
  ]
}

variable "fvc_gpt_callback_urls" {
  description = "Custom GPT の callback URL。GPT を作り直すと変わるので、その都度差し替える"
  type        = list(string)
  default = [
    "https://chat.openai.com/aip/g-540fe18a1e370f1ea96e70fa5e1ac0b00a699ece/oauth/callback",
    "https://chat.openai.com/aip/g-6a0c649e207c8191ade6ea82d335bf2e/oauth/callback",
    "https://chatgpt.com/aip/g-540fe18a1e370f1ea96e70fa5e1ac0b00a699ece/oauth/callback",
    "https://chatgpt.com/aip/g-6a0c649e207c8191ade6ea82d335bf2e/oauth/callback",
  ]
}

# -----------------------------------------------------------------------------
# memory-nest（統合先として受け入れる）
# -----------------------------------------------------------------------------

variable "memory_nest_callback_urls" {
  description = "memory-nest Web の callback URL"
  type        = list(string)
  default = [
    "http://localhost:3000/auth/callback",
    "https://example-app-6.cloudfront.net/auth/callback",
  ]
}

variable "memory_nest_logout_urls" {
  description = "memory-nest Web の logout URL"
  type        = list(string)
  default = [
    "http://localhost:3000/login",
    "https://example-app-6.cloudfront.net/login",
  ]
}
