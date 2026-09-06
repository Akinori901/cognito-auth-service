# =============================================================================
# auth-user-pool — 共通 Cognito 認証基盤（中立プール）
# =============================================================================
#
# 各アプリ(SkillLogger を皮切りに、将来 money-pilot / fair-value 等)を
# App Client 単位で収容する中立の共通ユーザープール。
# 特定アプリ名に依存しない命名・文面にしてある。
#
# 移植元: fair-value-calculator/terraform/cognito.tf（中立化 + GPT client 除去 +
# 初期管理者ユーザー作成を追加）。
# -----------------------------------------------------------------------------

# -----------------------------------------------------------------------------
# User Pool
# -----------------------------------------------------------------------------

resource "aws_cognito_user_pool" "main" {
  name = var.pool_name

  # email でサインイン
  username_attributes      = ["email"]
  auto_verified_attributes = ["email"]

  password_policy {
    minimum_length    = 8
    require_uppercase = true
    require_lowercase = true
    require_numbers   = true
    require_symbols   = false
  }

  account_recovery_setting {
    recovery_mechanism {
      name     = "verified_email"
      priority = 1
    }
  }

  # signup は admin 招待のみ。
  admin_create_user_config {
    allow_admin_create_user_only = true

    # 招待メール文面（中立。特定アプリ名を出さない）。
    # {username} = email、{####} = Cognito 自動生成の仮パスワード。両プレースホルダ必須。
    # 本文は HTML として送信される（Cognito 既定）。改行は <br> が必要なため、
    # プレーンテキストではなく HTML でマークアップしている。
    #
    # このプールは複数アプリ共通なので、どのアプリの招待かは本文だけでは
    # 特定できない。以前は収容アプリの URL を列挙していたが、アプリを
    # 増やすたびにここを直す必要があり、実際 3 アプリのまま古くなっていた。
    # いまは認証コンソールへ誘導し、**その人が許可されたアプリだけ**を
    # ログイン後に見せる（frontend の MyApps）。ここは変更不要になった。
    invite_message_template {
      email_subject = "【Auth】アカウント招待 / Your account invitation"
      email_message = <<-EOT
        <p>本サービスをご利用いただきありがとうございます。<br>
        管理者があなたのアカウントを作成しました。</p>

        <p><b>1. ログイン画面を開く</b></p>

        <p><a href="https://YOUR_CONSOLE_DOMAIN.cloudfront.net/">https://YOUR_CONSOLE_DOMAIN.cloudfront.net/</a></p>

        <p>ログインすると、ご利用いただけるサービスの一覧が表示されます。</p>

        <p><b>2. 下記の情報を入力する</b></p>

        <p>
          ユーザー名 (Username)：<b>{username}</b><br>
          仮パスワード (Temporary password)：<b>{####}</b>
        </p>

        <p>初回ログイン後、パスワードの変更を求められます。<br>
        仮パスワードは一定期間で失効しますので、お早めにログインしてください。</p>

        <p>このメールに心当たりがない場合は、本メールを破棄してください。</p>

        <hr>

        <p><b>English</b></p>

        <p>An administrator has created an account for you.<br>
        Open the console below and sign in. You will then see the services
        available to you.</p>

        <p><a href="https://YOUR_CONSOLE_DOMAIN.cloudfront.net/">https://YOUR_CONSOLE_DOMAIN.cloudfront.net/</a></p>

        <p>
          Username：<b>{username}</b><br>
          Temporary password：<b>{####}</b>
        </p>

        <p>You will be asked to change your password on first sign-in.<br>
        If you did not expect this email, please simply discard it.</p>

        <p>-- Auth Console</p>
      EOT
      sms_message   = "Auth: username {username}, temp password {####}"
    }
  }

  # 招待メールの送信元。var.ses_domain が空なら既定送信のまま
  # （ses.tf 参照）。既定は 1 日 50 通・共用ドメインで到達率が低い。
  dynamic "email_configuration" {
    for_each = local.ses_enabled ? [1] : []
    content {
      email_sending_account = "DEVELOPER"
      source_arn            = aws_sesv2_email_identity.main[0].arn
      from_email_address    = "${var.ses_from_display_name} <${local.ses_from}>"
      # 返信先は送信元と同じにしておく。no-reply でも受信自体は
      # メールサーバー側の設定次第なので、ここでは分けない。
      reply_to_email_address = local.ses_from
    }
  }

  deletion_protection = "ACTIVE"

  # 招待制ゲート: admin 事前作成メールのみサインアップ許可（presignup.tf 参照）
  lambda_config {
    pre_sign_up = aws_lambda_function.presignup.arn
  }

  schema {
    name                     = "name"
    attribute_data_type      = "String"
    required                 = true
    mutable                  = true
    developer_only_attribute = false

    string_attribute_constraints {
      min_length = 1
      max_length = 256
    }
  }

  tags = {
    Name = var.pool_name
  }
}

# -----------------------------------------------------------------------------
# User Pool Domain (Cognito prefix domain / Hosted UI)
# -----------------------------------------------------------------------------

resource "aws_cognito_user_pool_domain" "main" {
  domain       = var.domain_prefix
  user_pool_id = aws_cognito_user_pool.main.id
}

# -----------------------------------------------------------------------------
# Identity Provider: Google
# -----------------------------------------------------------------------------
#
# 事前に Google Cloud Console で OAuth 2.0 Client ID を作成する:
#   - 種類: ウェブアプリケーション
#   - 承認済みリダイレクト URI:
#     https://${var.domain_prefix}.auth.${var.aws_region}.amazoncognito.com/oauth2/idpresponse
# 取得した client_id / client_secret を terraform.tfvars に投入する。

resource "aws_cognito_identity_provider" "google" {
  user_pool_id  = aws_cognito_user_pool.main.id
  provider_name = "Google"
  provider_type = "Google"

  provider_details = {
    client_id                     = var.google_oauth_client_id
    client_secret                 = var.google_oauth_client_secret
    authorize_scopes              = "openid email profile"
    attributes_url                = "https://people.googleapis.com/v1/people/me?personFields="
    attributes_url_add_attributes = "true"
    authorize_url                 = "https://accounts.google.com/o/oauth2/v2/auth"
    oidc_issuer                   = "https://accounts.google.com"
    token_request_method          = "POST"
    token_url                     = "https://www.googleapis.com/oauth2/v4/token"
  }

  attribute_mapping = {
    email          = "email"
    email_verified = "email_verified"
    name           = "name"
    username       = "sub"
  }
}

# -----------------------------------------------------------------------------
# App Client: SkillLogger (SPA, PKCE, public client)
# -----------------------------------------------------------------------------
#
# 別アプリ(money-pilot 等)を収容する際は、この resource を複製して
# name / callback_urls / logout_urls を差し替えた App Client を追加する。

resource "aws_cognito_user_pool_client" "skilllogger_web" {
  name         = "skilllogger-web-client"
  user_pool_id = aws_cognito_user_pool.main.id

  generate_secret = false # SPA なので client_secret は持たない (PKCE で守る)

  allowed_oauth_flows_user_pool_client = true
  allowed_oauth_flows                  = ["code"]
  allowed_oauth_scopes                 = ["openid", "profile", "email"]
  supported_identity_providers = [
    "COGNITO",
    aws_cognito_identity_provider.google.provider_name,
  ]

  callback_urls = var.skilllogger_callback_urls
  logout_urls   = var.skilllogger_logout_urls

  prevent_user_existence_errors = "ENABLED"

  explicit_auth_flows = [
    "ALLOW_REFRESH_TOKEN_AUTH",
    "ALLOW_USER_SRP_AUTH",
  ]

  access_token_validity  = 1
  id_token_validity      = 1
  refresh_token_validity = 30
  token_validity_units {
    access_token  = "hours"
    id_token      = "hours"
    refresh_token = "days"
  }
}

# -----------------------------------------------------------------------------
# App Client: money-pilot (SPA, PKCE, public client)
# -----------------------------------------------------------------------------
#
# skilllogger_web と同じ構成。収容アプリごとに App Client を分けることで、
# JWT の client_id / callback URL をアプリ単位で制御する。

resource "aws_cognito_user_pool_client" "money_pilot_web" {
  name         = "money-pilot-web-client"
  user_pool_id = aws_cognito_user_pool.main.id

  generate_secret = false # SPA なので client_secret は持たない (PKCE で守る)

  allowed_oauth_flows_user_pool_client = true
  allowed_oauth_flows                  = ["code"]
  allowed_oauth_scopes                 = ["openid", "profile", "email"]
  supported_identity_providers = [
    "COGNITO",
    aws_cognito_identity_provider.google.provider_name,
  ]

  callback_urls = var.money_pilot_callback_urls
  logout_urls   = var.money_pilot_logout_urls

  prevent_user_existence_errors = "ENABLED"

  explicit_auth_flows = [
    "ALLOW_REFRESH_TOKEN_AUTH",
    "ALLOW_USER_SRP_AUTH",
  ]

  access_token_validity  = 1
  id_token_validity      = 1
  refresh_token_validity = 30
  token_validity_units {
    access_token  = "hours"
    id_token      = "hours"
    refresh_token = "days"
  }
}

# -----------------------------------------------------------------------------
# App Client: task-scope (SPA, PKCE, public client)
# -----------------------------------------------------------------------------
#
# skilllogger_web / money_pilot_web と同じ構成。収容アプリごとに App Client を分けることで、
# JWT の client_id / callback URL をアプリ単位で制御する。

resource "aws_cognito_user_pool_client" "task_scope_web" {
  name         = "task-scope-web-client"
  user_pool_id = aws_cognito_user_pool.main.id

  generate_secret = false # SPA なので client_secret は持たない (PKCE で守る)

  allowed_oauth_flows_user_pool_client = true
  allowed_oauth_flows                  = ["code"]
  allowed_oauth_scopes                 = ["openid", "profile", "email"]
  supported_identity_providers = [
    "COGNITO",
    aws_cognito_identity_provider.google.provider_name,
  ]

  callback_urls = var.task_scope_callback_urls
  logout_urls   = var.task_scope_logout_urls

  prevent_user_existence_errors = "ENABLED"

  explicit_auth_flows = [
    "ALLOW_REFRESH_TOKEN_AUTH",
    "ALLOW_USER_SRP_AUTH",
    "ALLOW_USER_PASSWORD_AUTH",
  ]

  access_token_validity  = 1
  id_token_validity      = 1
  refresh_token_validity = 30
  token_validity_units {
    access_token  = "hours"
    id_token      = "hours"
    refresh_token = "days"
  }
}

# -----------------------------------------------------------------------------
# App Client: 認証コンソール (auth-console)
# -----------------------------------------------------------------------------
#
# 「誰が・どのアプリを・どの役割で・どの範囲まで」を設定する管理コンソール自身の
# ログイン口。他の App Client と違い、**Google ログインのみ**を許可する。
#
# supported_identity_providers から COGNITO を外しているのがその実装。
# Cognito ネイティブ（メール＋パスワード）でこの Client には来られない。
# 加えてバックエンド側でも identities claim の providerName == "Google" を検証する
# （二重の関門。Client 設定だけに頼らない）。

resource "aws_cognito_user_pool_client" "auth_console_web" {
  name         = "auth-console-web-client"
  user_pool_id = aws_cognito_user_pool.main.id

  generate_secret = false # SPA なので client_secret は持たない (PKCE で守る)

  allowed_oauth_flows_user_pool_client = true
  allowed_oauth_flows                  = ["code"]
  allowed_oauth_scopes                 = ["openid", "profile", "email"]

  # ★ COGNITO を含めない = ネイティブログイン不可（Google 専用）
  supported_identity_providers = [
    aws_cognito_identity_provider.google.provider_name,
  ]

  callback_urls = var.auth_console_callback_urls
  logout_urls   = var.auth_console_logout_urls

  prevent_user_existence_errors = "ENABLED"

  # ネイティブ認証フローを持たせない。リフレッシュのみ許可する。
  explicit_auth_flows = [
    "ALLOW_REFRESH_TOKEN_AUTH",
  ]

  access_token_validity  = 1
  id_token_validity      = 1
  refresh_token_validity = 30
  token_validity_units {
    access_token  = "hours"
    id_token      = "hours"
    refresh_token = "days"
  }
}

# -----------------------------------------------------------------------------
# 初期管理者ユーザー（締め出し防止の初期アカウント）
# -----------------------------------------------------------------------------
#
# Google ログインするアドレスと同じメールで Cognito ネイティブユーザーを先に作っておく。
# これにより、万一 Google 連携でつまずいても仮パスワードでのログイン経路が残る（保険）。
# アプリ側 DB の allowed_email 初期レコードは backend 側で同じメールを登録する。
#
# message_action = SUPPRESS: 招待メールを送らない（自分用の初期作成のため）。
# 初回ログイン時に FORCE_CHANGE_PASSWORD となるため、仮パスワードは AWS Console から
# admin-set-user-password で設定するか、SUPPRESS を外して招待メールを受け取る。

resource "aws_cognito_user" "initial_admin" {
  user_pool_id = aws_cognito_user_pool.main.id
  username     = var.initial_admin_email

  attributes = {
    email          = var.initial_admin_email
    email_verified = "true"
    name           = "admin"
  }

  # 初期作成時は招待メールを抑制（自分用のため）。Google ログインを主経路とする。
  message_action = "SUPPRESS"

  # email/name はユーザー自身が後で変えうるので drift 管理外にする。
  lifecycle {
    ignore_changes = [attributes]
  }
}

# -----------------------------------------------------------------------------
# App Client: fair-value-calculator (SPA, PKCE, public client)
# -----------------------------------------------------------------------------
#
# FVC を fvc-user-pool から移してくるための受け皿。
# 作った時点では誰も使わないので、既存アプリへの影響は無い。
# 切り替えは FVC 側の環境変数を差し替えるタイミングで起きる。

resource "aws_cognito_user_pool_client" "fvc_web" {
  name         = "fvc-web-client"
  user_pool_id = aws_cognito_user_pool.main.id

  generate_secret = false # SPA なので client_secret は持たない (PKCE で守る)

  allowed_oauth_flows_user_pool_client = true
  allowed_oauth_flows                  = ["code"]
  allowed_oauth_scopes                 = ["openid", "profile", "email"]
  supported_identity_providers = [
    "COGNITO",
    aws_cognito_identity_provider.google.provider_name,
  ]

  callback_urls = var.fvc_callback_urls
  logout_urls   = var.fvc_logout_urls

  prevent_user_existence_errors = "ENABLED"

  explicit_auth_flows = [
    "ALLOW_REFRESH_TOKEN_AUTH",
    "ALLOW_USER_SRP_AUTH",
  ]

  access_token_validity  = 1
  id_token_validity      = 1
  refresh_token_validity = 30
  token_validity_units {
    access_token  = "hours"
    id_token      = "hours"
    refresh_token = "days"
  }
}

# -----------------------------------------------------------------------------
# App Client: fair-value-calculator / Custom GPT (confidential client)
# -----------------------------------------------------------------------------
#
# **このプールで唯一 client_secret を持つ Client。**
# Custom GPT（ChatGPT の Actions）が OAuth のクライアントとして振る舞うため、
# SPA と違って secret を要求する。
#
# secret は Terraform の output に出さない（tfstate には入るが、
# 意図的に画面や CI へ流さない）。取得は必要なときに CLI で行う:
#
#   aws cognito-idp describe-user-pool-client \
#     --user-pool-id <pool> --client-id <id> \
#     --query 'UserPoolClient.ClientSecret' --output text
#
# 認証コンソールからこの Client を作成・削除する機能は用意しない。
# secret を安全に渡す方法が無く（Cognito は後から何度でも取得できるため
# 「一度だけ表示」も緩和にならない）、削除は当該アプリのログインを
# 即座に全停止させるため。誰が使ってよいかは Grant で管理する。

resource "aws_cognito_user_pool_client" "fvc_gpt" {
  name         = "fvc-gpt-client"
  user_pool_id = aws_cognito_user_pool.main.id

  generate_secret = true # Custom GPT は client_secret を要求する

  allowed_oauth_flows_user_pool_client = true
  allowed_oauth_flows                  = ["code"]
  allowed_oauth_scopes                 = ["openid", "profile", "email"]
  supported_identity_providers = [
    "COGNITO",
    aws_cognito_identity_provider.google.provider_name,
  ]

  callback_urls = var.fvc_gpt_callback_urls

  prevent_user_existence_errors = "ENABLED"

  explicit_auth_flows = [
    "ALLOW_REFRESH_TOKEN_AUTH",
    "ALLOW_USER_SRP_AUTH",
  ]

  access_token_validity  = 1
  id_token_validity      = 1
  refresh_token_validity = 30
  token_validity_units {
    access_token  = "hours"
    id_token      = "hours"
    refresh_token = "days"
  }
}

# -----------------------------------------------------------------------------
# App Client: memory-nest (Web / SPA)
# -----------------------------------------------------------------------------
#
# memory-nest を専用プール（memory-nest-users）から移してくる受け皿。
# Hosted UI は使わず Amplify の SRP 認証で入るため、callback URL は
# 実質使われないが、将来 Hosted UI に寄せる場合に備えて登録しておく。

resource "aws_cognito_user_pool_client" "memory_nest_web" {
  name         = "memory-nest-web-client"
  user_pool_id = aws_cognito_user_pool.main.id

  generate_secret = false # SPA なので client_secret は持たない

  allowed_oauth_flows_user_pool_client = true
  allowed_oauth_flows                  = ["code"]
  allowed_oauth_scopes                 = ["openid", "profile", "email"]
  supported_identity_providers = [
    "COGNITO",
    aws_cognito_identity_provider.google.provider_name,
  ]

  callback_urls = var.memory_nest_callback_urls
  logout_urls   = var.memory_nest_logout_urls

  prevent_user_existence_errors = "ENABLED"

  # SRP に加えて USER_PASSWORD_AUTH も許可する。
  # Flutter アプリ側が userPassword フローを使うため。
  explicit_auth_flows = [
    "ALLOW_REFRESH_TOKEN_AUTH",
    "ALLOW_USER_SRP_AUTH",
    "ALLOW_USER_PASSWORD_AUTH",
  ]

  access_token_validity  = 1
  id_token_validity      = 1
  refresh_token_validity = 30
  token_validity_units {
    access_token  = "hours"
    id_token      = "hours"
    refresh_token = "days"
  }
}

# -----------------------------------------------------------------------------
# App Client: memory-nest (Flutter アプリ)
# -----------------------------------------------------------------------------
#
# モバイルはカスタムスキームで戻る。Web と分けるのは、
# callback URL の性質が違い、片方の変更が他方に影響しないようにするため。

resource "aws_cognito_user_pool_client" "memory_nest_app" {
  name         = "memory-nest-app-client"
  user_pool_id = aws_cognito_user_pool.main.id

  generate_secret = false

  allowed_oauth_flows_user_pool_client = true
  allowed_oauth_flows                  = ["code"]
  allowed_oauth_scopes                 = ["openid", "profile", "email"]
  supported_identity_providers = [
    "COGNITO",
    aws_cognito_identity_provider.google.provider_name,
  ]

  callback_urls = ["memorynest://callback"]
  logout_urls   = ["memorynest://signout"]

  prevent_user_existence_errors = "ENABLED"

  explicit_auth_flows = [
    "ALLOW_REFRESH_TOKEN_AUTH",
    "ALLOW_USER_SRP_AUTH",
    "ALLOW_USER_PASSWORD_AUTH",
  ]

  access_token_validity  = 1
  id_token_validity      = 1
  refresh_token_validity = 30
  token_validity_units {
    access_token  = "hours"
    id_token      = "hours"
    refresh_token = "days"
  }
}
