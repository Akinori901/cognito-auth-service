# SkillLogger（および将来の各アプリ）の env / Secrets に投入する値。
# apply 後に `terraform output` で確認し、各アプリの settings / .env に設定する。

output "user_pool_id" {
  description = "Cognito User Pool ID（例: ap-northeast-1_xxxxxxxxx）。各アプリの COGNITO_USER_POOL_ID / VITE_COGNITO_USER_POOL_ID に設定"
  value       = aws_cognito_user_pool.main.id
}

output "user_pool_arn" {
  description = "Cognito User Pool ARN"
  value       = aws_cognito_user_pool.main.arn
}

output "issuer" {
  description = "JWT issuer（backend の COGNITO_JWT_ISSUER 派生元）"
  value       = "https://cognito-idp.${var.aws_region}.amazonaws.com/${aws_cognito_user_pool.main.id}"
}

output "jwks_url" {
  description = "JWKS エンドポイント"
  value       = "https://cognito-idp.${var.aws_region}.amazonaws.com/${aws_cognito_user_pool.main.id}/.well-known/jwks.json"
}

output "skilllogger_web_client_id" {
  description = "SkillLogger 用 App Client ID。COGNITO_WEB_CLIENT_ID / VITE_COGNITO_WEB_CLIENT_ID に設定"
  value       = aws_cognito_user_pool_client.skilllogger_web.id
}

output "money_pilot_web_client_id" {
  description = "MoneyPilot 用 App Client ID。COGNITO_WEB_CLIENT_ID / VITE_COGNITO_WEB_CLIENT_ID に設定"
  value       = aws_cognito_user_pool_client.money_pilot_web.id
}

output "task_scope_web_client_id" {
  description = "task-scope 用 App Client ID。COGNITO_WEB_CLIENT_ID / VITE_COGNITO_WEB_CLIENT_ID に設定"
  value       = aws_cognito_user_pool_client.task_scope_web.id
}

output "hosted_ui_domain" {
  description = "Cognito Hosted UI ドメイン。VITE_COGNITO_DOMAIN_PREFIX に設定（prefix 部分）"
  value       = "${var.domain_prefix}.auth.${var.aws_region}.amazoncognito.com"
}

output "domain_prefix" {
  description = "Hosted UI ドメイン prefix（VITE_COGNITO_DOMAIN_PREFIX）"
  value       = var.domain_prefix
}

output "google_redirect_uri" {
  description = "Google Cloud Console の「承認済みリダイレクト URI」に登録する値"
  value       = "https://${var.domain_prefix}.auth.${var.aws_region}.amazoncognito.com/oauth2/idpresponse"
}

# -----------------------------------------------------------------------------
# 認証コンソール
# -----------------------------------------------------------------------------

output "auth_console_web_client_id" {
  description = "認証コンソール用 App Client ID（frontend の VITE_COGNITO_* に設定する）"
  value       = aws_cognito_user_pool_client.auth_console_web.id
}

output "auth_tables" {
  description = "認証コンソールの DynamoDB テーブル名（backend の環境変数に設定する）"
  value = {
    users      = aws_dynamodb_table.auth_users.name
    grants     = aws_dynamodb_table.auth_grants.name
    identities = aws_dynamodb_table.auth_identities.name
  }
}

output "auth_console_url" {
  description = "認証コンソールの URL"
  value       = "https://${aws_cloudfront_distribution.console.domain_name}"
}

output "auth_console_deploy_targets" {
  description = "CD が使うデプロイ先"
  value = {
    artifacts_bucket = aws_s3_bucket.lambda_artifacts.bucket
    front_bucket     = aws_s3_bucket.console_front.bucket
    lambda_function  = aws_lambda_function.api.function_name
    distribution_id  = aws_cloudfront_distribution.console.id
  }
}

# -----------------------------------------------------------------------------
# SES（招待メールの送信元）
# -----------------------------------------------------------------------------

output "ses_enabled" {
  description = "SES を使う設定になっているか。false なら Cognito 既定送信（1 日 50 通・到達率低）"
  value       = local.ses_enabled
}

output "ses_from_address" {
  description = "招待メールの送信元アドレス"
  value       = local.ses_enabled ? local.ses_from : "(Cognito 既定送信)"
}

# DNS が外部（ヘテムル等）にある場合、この 3 件を手で登録する。
# 登録しないと DKIM が有効にならず、SES の到達率の利点が得られない。
output "ses_dkim_records" {
  description = "DNS に登録する DKIM の CNAME 3 件（ses_manage_dns = false のとき手動登録）"
  value = local.ses_enabled && !local.ses_dns_managed ? [
    for t in aws_sesv2_email_identity.main[0].dkim_signing_attributes[0].tokens : {
      name  = "${t}._domainkey.${var.ses_domain}"
      type  = "CNAME"
      value = "${t}.dkim.amazonses.com"
    }
  ] : []
}

# SPF は 1 ドメインに 1 つしか置けない。**新規に作らず既存に追記する。**
# 2 つあると全部無効になり、既存のメール送信まで到達率が落ちる。
output "ses_spf_hint" {
  description = "既存の SPF レコードに追記する内容（新規作成しないこと）"
  value = local.ses_enabled ? join("", [
    "既存の TXT レコード（v=spf1 で始まるもの）に 'include:amazonses.com' を追記する。",
    "例: v=spf1 include:_spf.example.jp include:amazonses.com ~all"
  ]) : ""
}

# -----------------------------------------------------------------------------
# fair-value-calculator
# -----------------------------------------------------------------------------

output "fvc_web_client_id" {
  description = "FVC Web 用 App Client ID。COGNITO_WEB_CLIENT_ID / VITE_COGNITO_WEB_CLIENT_ID に設定"
  value       = aws_cognito_user_pool_client.fvc_web.id
}

output "fvc_gpt_client_id" {
  description = "FVC Custom GPT 用 App Client ID。COGNITO_GPT_CLIENT_ID に設定"
  value       = aws_cognito_user_pool_client.fvc_gpt.id
}

# client_secret は出力しない。必要なときに CLI で取得する:
#   aws cognito-idp describe-user-pool-client --user-pool-id <pool> \
#     --client-id <fvc_gpt_client_id> --query 'UserPoolClient.ClientSecret' --output text

output "memory_nest_web_client_id" {
  description = "memory-nest Web 用 App Client ID"
  value       = aws_cognito_user_pool_client.memory_nest_web.id
}

output "memory_nest_app_client_id" {
  description = "memory-nest Flutter アプリ用 App Client ID"
  value       = aws_cognito_user_pool_client.memory_nest_app.id
}
