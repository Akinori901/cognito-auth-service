# =============================================================================
# Pre-Sign-Up トリガー — 招待制ゲート
# =============================================================================
# admin が事前作成した email のみサインアップ（Google 連携含む）を許可し、
# 未招待の Google アカウントを拒否する。全ツール共通の auth-user-pool に効く。
# -----------------------------------------------------------------------------

# Lambda ソースを zip 化
data "archive_file" "presignup" {
  type        = "zip"
  source_dir  = "${path.module}/lambda/presignup"
  output_path = "${path.module}/build/presignup.zip"
}

# 実行ロール
resource "aws_iam_role" "presignup" {
  name = "auth-user-pool-presignup"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

# 基本実行（CloudWatch Logs）
resource "aws_iam_role_policy_attachment" "presignup_basic" {
  role       = aws_iam_role.presignup.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# Cognito 操作（ユーザー検索・外部IdPリンク）
resource "aws_iam_role_policy" "presignup_cognito" {
  name = "auth-user-pool-presignup-cognito"
  role = aws_iam_role.presignup.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "cognito-idp:ListUsers",
        "cognito-idp:AdminLinkProviderForUser",
        "cognito-idp:AdminGetUser",
      ]
      Resource = aws_cognito_user_pool.main.arn
    }]
  })
}

# Lambda 関数
resource "aws_lambda_function" "presignup" {
  function_name    = "auth-user-pool-presignup"
  role             = aws_iam_role.presignup.arn
  handler          = "index.lambda_handler"
  runtime          = "python3.13"
  architectures    = ["arm64"]
  timeout          = 10
  filename         = data.archive_file.presignup.output_path
  source_code_hash = data.archive_file.presignup.output_base64sha256
}

# Cognito がこの Lambda を呼び出す権限
resource "aws_lambda_permission" "presignup_cognito" {
  statement_id  = "AllowCognitoInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.presignup.function_name
  principal     = "cognito-idp.amazonaws.com"
  source_arn    = aws_cognito_user_pool.main.arn
}
