# -----------------------------------------------------------------------------
# 認証コンソールの API（Lambda + API Gateway）
# -----------------------------------------------------------------------------
#
# Go のバイナリを Lambda のカスタムランタイム（provided.al2023）で動かす。
# コンテナイメージではなく zip にしたのは、ECR リポジトリを増やさずに済み、
# 起動も速いため。バイナリ 1 つなので zip で十分。
#
# ビルドは CD（GitHub Actions）が行い、成果物を S3 経由で渡す。
# ローカルからの apply では、S3 に上がっている最新版がそのまま使われる。

# --- ビルド成果物の置き場 -----------------------------------------------------

resource "aws_s3_bucket" "lambda_artifacts" {
  bucket = "auth-console-artifacts-${data.aws_caller_identity.current.account_id}"

  tags = {
    Name = "auth-console-artifacts"
  }
}

resource "aws_s3_bucket_public_access_block" "lambda_artifacts" {
  bucket                  = aws_s3_bucket.lambda_artifacts.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# 古いビルド成果物を溜めない。ロールバック用に 30 日だけ残す。
resource "aws_s3_bucket_lifecycle_configuration" "lambda_artifacts" {
  bucket = aws_s3_bucket.lambda_artifacts.id

  rule {
    id     = "expire-old-artifacts"
    status = "Enabled"

    filter {}

    noncurrent_version_expiration {
      noncurrent_days = 30
    }
  }
}

resource "aws_s3_bucket_versioning" "lambda_artifacts" {
  bucket = aws_s3_bucket.lambda_artifacts.id
  versioning_configuration {
    status = "Enabled"
  }
}

data "aws_caller_identity" "current" {}

# --- 実行ロール ---------------------------------------------------------------

resource "aws_iam_role" "api" {
  name = "auth-console-lambda"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "api_basic" {
  role       = aws_iam_role.api.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# DynamoDB の 3 テーブルだけに絞る。
# 認可設定を持つ表なので、他のリソースへの権限は一切与えない。
resource "aws_iam_role_policy" "api_dynamodb" {
  name = "auth-console-dynamodb"
  role = aws_iam_role.api.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "dynamodb:GetItem",
        "dynamodb:PutItem",
        "dynamodb:DeleteItem",
        "dynamodb:Query",
        "dynamodb:Scan",
      ]
      Resource = [
        aws_dynamodb_table.auth_users.arn,
        aws_dynamodb_table.auth_grants.arn,
        aws_dynamodb_table.auth_identities.arn,
        "${aws_dynamodb_table.auth_identities.arn}/index/*",
      ]
    }]
  })
}

# Cognito ユーザープールの操作。コンソールから招待・状態確認を行うために要る。
#
# **絞り込みの要点**
#   - 対象はこのユーザープール 1 つだけ（Resource で固定）
#   - AdminDeleteUser を与えない。消すと Google のリンクも失われ、
#     同じ email で作り直しても別人になる。無効化で足りる。
#   - AdminSetUserPassword を与えない。コンソールがパスワードを
#     知る経路を作らない（仮パスワードは Cognito が本人にだけ送る）。
resource "aws_iam_role_policy" "api_cognito" {
  name = "auth-console-cognito"
  role = aws_iam_role.api.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "cognito-idp:ListUsers",
        # AdminCreateUser は新規作成と招待の再送（MessageAction=RESEND）の両方に使う
        "cognito-idp:AdminCreateUser",
        "cognito-idp:AdminGetUser",
        "cognito-idp:AdminEnableUser",
        "cognito-idp:AdminDisableUser",
        # 仮パスワードの有効期間（UnusedAccountValidityDays）を読み、
        # 失効までの残日数を出すために要る
        "cognito-idp:DescribeUserPool",
        # 登録アプリ（App Client）の一覧表示。読み取りのみで、
        # 作成・削除は与えない（Terraform の担当）。
        # ListUserPoolClients は client_secret を返さない。
        "cognito-idp:ListUserPoolClients",
        "cognito-idp:DescribeUserPoolClient",
      ]
      Resource = aws_cognito_user_pool.main.arn
    }]
  })
}

# 共有シークレット（各アプリの候補一覧 API を叩くときのヘッダ値）を読む。
# 値そのものは Terraform で管理しない。tfstate に平文で残るのを避けるため、
# パラメータは手で作り、ここでは読む権限だけを与える。
resource "aws_iam_role_policy" "api_ssm" {
  name = "auth-console-ssm"
  role = aws_iam_role.api.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["ssm:GetParameter"]
      Resource = "arn:aws:ssm:${var.aws_region}:${data.aws_caller_identity.current.account_id}:parameter/auth-console/*"
    }]
  })
}

# --- Lambda -------------------------------------------------------------------

resource "aws_lambda_function" "api" {
  function_name = "auth-console-api"
  role          = aws_iam_role.api.arn

  # Go は自前で起動できるのでカスタムランタイムを使う。
  # ハンドラ名は bootstrap 固定（provided.al2023 の規約）。
  runtime = "provided.al2023"
  handler = "bootstrap"

  # arm64 の方が安く、Go は問題なく動く。
  architectures = ["arm64"]

  s3_bucket = aws_s3_bucket.lambda_artifacts.bucket
  s3_key    = var.lambda_artifact_key

  timeout     = 10
  memory_size = 256

  environment {
    variables = {
      COGNITO_USER_POOL_ID  = aws_cognito_user_pool.main.id
      COGNITO_CLIENT_ID     = aws_cognito_user_pool_client.auth_console_web.id
      AUTH_USERS_TABLE      = aws_dynamodb_table.auth_users.name
      AUTH_GRANTS_TABLE     = aws_dynamodb_table.auth_grants.name
      AUTH_IDENTITIES_TABLE = aws_dynamodb_table.auth_identities.name
      # 緊急用の抜け道。Google IdP が使えなくなったときだけ設定する。
      # **通常は空にしておくこと。**
      BREAK_GLASS_EMAIL = var.break_glass_email

      # 各アプリの候補一覧 API を叩くときの共有シークレットの置き場。
      # 値そのものは環境変数に持たない（Lambda の設定から平文が見えるため）。
      CONSOLE_SECRET_PARAM = var.console_secret_param

      # スコープ候補の取得先。未設定のアプリは「候補なし」になる。
      SCOPE_ENDPOINT_TASK_SCOPE  = var.scope_endpoint_task_scope
      SCOPE_ENDPOINT_MONEY_PILOT = var.scope_endpoint_money_pilot
    }
  }

  # CD がコードを更新するため、Terraform はコードの変更を追わない。
  # ここを外すと apply のたびに古い版へ巻き戻る。
  lifecycle {
    ignore_changes = [s3_key]
  }
}

resource "aws_cloudwatch_log_group" "api" {
  name              = "/aws/lambda/${aws_lambda_function.api.function_name}"
  retention_in_days = 30
}

# --- API Gateway (HTTP API) ---------------------------------------------------

resource "aws_apigatewayv2_api" "api" {
  name          = "auth-console"
  protocol_type = "HTTP"

  # CORS は設定しない。フロントは CloudFront 経由で同一オリジンになるため。
  # ここを開けると、この API を任意のサイトから叩けるようになる。
}

resource "aws_apigatewayv2_integration" "api" {
  api_id                 = aws_apigatewayv2_api.api.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_function.api.invoke_arn
  payload_format_version = "2.0"
}

# ルーティングは Go 側（chi）が持つので、ここは全部を Lambda に流す。
resource "aws_apigatewayv2_route" "proxy" {
  api_id    = aws_apigatewayv2_api.api.id
  route_key = "$default"
  target    = "integrations/${aws_apigatewayv2_integration.api.id}"
}

resource "aws_apigatewayv2_stage" "default" {
  api_id      = aws_apigatewayv2_api.api.id
  name        = "$default"
  auto_deploy = true

  access_log_settings {
    destination_arn = aws_cloudwatch_log_group.api_access.arn
    format = jsonencode({
      requestId = "$context.requestId"
      method    = "$context.httpMethod"
      path      = "$context.path"
      status    = "$context.status"
      error     = "$context.error.message"
    })
  }
}

resource "aws_cloudwatch_log_group" "api_access" {
  name              = "/aws/apigateway/auth-console"
  retention_in_days = 30
}

resource "aws_lambda_permission" "api" {
  statement_id  = "AllowAPIGatewayInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.api.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.api.execution_arn}/*/*"
}
