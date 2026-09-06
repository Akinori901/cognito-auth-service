# -----------------------------------------------------------------------------
# 認証コンソールのデータストア
# -----------------------------------------------------------------------------
#
# 「誰が・どのアプリを・どの役割で・どの範囲まで」を保持する中央テーブル群。
#
# DynamoDB を選んだ理由:
#   - 認証は全リクエストの入口なので、同時実行に強い必要がある
#   - VPC も EFS も要らないため、全サービスの Lambda から直接叩ける
#   - 件数が少なくアクセスも認証時のみなので PAY_PER_REQUEST が安い
#
# キーを email にした理由:
#   同一 email に Cognito sub が複数ぶら下がる（Google 経由と Cognito 直接）。
#   sub をキーにすると、ログイン経路が違うだけで別人扱いになってしまう。
#   既存 4 サービスもすべて email をキーにしている（m_user_allowed_emails /
#   MemberSpaceAccess）ため、中央もそれに揃える。

locals {
  # 誤削除に備えて本番テーブルは point-in-time recovery を有効にする。
  # 認可設定が消えると全サービスに入れなくなるため、復旧手段は必須。
  auth_table_common = {
    billing_mode = "PAY_PER_REQUEST"
  }
}

# -----------------------------------------------------------------------------
# auth-users — 中央のユーザー台帳
# -----------------------------------------------------------------------------
#
# 「共通認証基盤に乗り入れてよい人」の一覧。ここに行が無い人は、Cognito に
# アカウントがあっても各サービスに入れない（Phase 4 以降）。

resource "aws_dynamodb_table" "auth_users" {
  name         = "auth-users"
  billing_mode = local.auth_table_common.billing_mode
  hash_key     = "email"

  attribute {
    name = "email"
    type = "S"
  }

  point_in_time_recovery {
    enabled = true
  }

  # 認可設定が消えると全サービスに入れなくなる。誤削除を防ぐ。
  deletion_protection_enabled = true

  tags = {
    Name = "auth-users"
  }
}

# -----------------------------------------------------------------------------
# auth-grants — アプリ利用可否とスコープ
# -----------------------------------------------------------------------------
#
# **行が存在すること自体が「このアプリを使ってよい」を意味する。**
# scopes は不透明な文字列の配列で、中央は意味を解釈しない。
# 「seed-tech がどのスペース ID か」の解決は task-scope 側の責任。
# これにより、アプリを増やしても中央を直さずに済む。

resource "aws_dynamodb_table" "auth_grants" {
  name         = "auth-grants"
  billing_mode = local.auth_table_common.billing_mode
  hash_key     = "email"
  range_key    = "app"

  attribute {
    name = "email"
    type = "S"
  }

  attribute {
    name = "app"
    type = "S"
  }

  point_in_time_recovery {
    enabled = true
  }

  deletion_protection_enabled = true

  tags = {
    Name = "auth-grants"
  }
}

# -----------------------------------------------------------------------------
# auth-identities — sub の履歴
# -----------------------------------------------------------------------------
#
# email をキーにする設計の弱点は「email が変わると設定が孤立する」こと。
# ログインのたびに sub → email を記録しておけば、email 変更後も辿れる。
# SkillLogger の m_cognito_links と同じ考え方。

resource "aws_dynamodb_table" "auth_identities" {
  name         = "auth-identities"
  billing_mode = local.auth_table_common.billing_mode
  hash_key     = "sub"

  attribute {
    name = "sub"
    type = "S"
  }

  # email から sub を引く（コンソールで「このユーザーのログイン履歴」を出す用）
  attribute {
    name = "email"
    type = "S"
  }

  global_secondary_index {
    name            = "byEmail"
    hash_key        = "email"
    projection_type = "ALL"
  }

  point_in_time_recovery {
    enabled = true
  }

  tags = {
    Name = "auth-identities"
  }
}
