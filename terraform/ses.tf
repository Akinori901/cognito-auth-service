# =============================================================================
# 招待メールの送信元（SES）
# =============================================================================
#
# Cognito の既定送信は 1 日 50 通の上限があり、送信元が AWS 利用者の共用
# ドメイン（no-reply@verificationemail.com）のため、企業のメールゲートウェイに
# 迷惑メール判定されやすい。社外の人を招待するには到達率が足りない。
#
# **var.ses_domain が空ならこのファイルは何も作らない。** 既定送信のまま動く。
# 公開版を試す人がドメインを持っていなくても apply できるようにするため。
#
# --- 既存のメール環境への影響 -------------------------------------------------
#
# ここで扱うのは **送信だけ**。受信（MX）には一切関与しない。
# ドメインのメールを別のサービス（レンタルサーバー等）で受けている場合も、
# MX を触らない限りメールボックスはそのまま動く。
#
# SPF だけは注意が要る。1 ドメインに 1 つしか置けないため、既存の SPF が
# あるなら **新規に作らず include を追記する**（2 つあると全部無効になる）。
# このファイルは SPF を作らない。手順は output の指示に従う。
# -----------------------------------------------------------------------------

locals {
  ses_enabled = var.ses_domain != ""
  # ses_manage_dns が true でもゾーン ID が無ければ DNS は作れない。
  # 設定漏れで apply が落ちるより、手動登録に倒す方が親切。
  ses_dns_managed = local.ses_enabled && var.ses_manage_dns && var.ses_route53_zone_id != ""
  ses_from        = local.ses_enabled ? "${var.ses_from_local_part}@${var.ses_domain}" : ""
}

# -----------------------------------------------------------------------------
# ドメイン ID（DKIM 署名を有効にする）
# -----------------------------------------------------------------------------
#
# Easy DKIM を使う。SES が鍵を持ち、公開鍵を CNAME 3 件で公開する方式。
# 受信側はこの署名でメールの正当性を検証するため、到達率が大きく変わる。

resource "aws_sesv2_email_identity" "main" {
  count = local.ses_enabled ? 1 : 0

  email_identity = var.ses_domain

  dkim_signing_attributes {
    next_signing_key_length = "RSA_2048_BIT"
  }
}

# -----------------------------------------------------------------------------
# DKIM の CNAME（Route53 にゾーンがある場合のみ）
# -----------------------------------------------------------------------------
#
# DNS が外部にある場合はここを作らず、output の値を手で登録する。
# レコード名は "<token>._domainkey.<domain>" という専用の名前空間なので、
# 既存のレコードと衝突しない。

resource "aws_route53_record" "ses_dkim" {
  count = local.ses_dns_managed ? 3 : 0

  zone_id = var.ses_route53_zone_id
  name    = "${aws_sesv2_email_identity.main[0].dkim_signing_attributes[0].tokens[count.index]}._domainkey.${var.ses_domain}"
  type    = "CNAME"
  ttl     = 600
  records = ["${aws_sesv2_email_identity.main[0].dkim_signing_attributes[0].tokens[count.index]}.dkim.amazonses.com"]

  # 既に同名のレコードがあっても上書きしてよい。DKIM トークンは
  # SES が発行するもので、他と用途が重ならない。
  allow_overwrite = true
}

# -----------------------------------------------------------------------------
# Cognito から SES を使う許可
# -----------------------------------------------------------------------------
#
# Cognito が送信元アドレスを名乗れるようにする。この権限が無いと
# 招待メールの送信が失敗する（ユーザー作成そのものは成功するため、
# 「作れたのにメールが来ない」という分かりにくい形で表面化する）。
#
# 条件付きで絞り込み、このアカウントの Cognito からのみ使えるようにする。

resource "aws_sesv2_email_identity_policy" "cognito" {
  count = local.ses_enabled ? 1 : 0

  email_identity = aws_sesv2_email_identity.main[0].email_identity
  policy_name    = "allow-cognito-send"

  policy = jsonencode({
    Version = "2008-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "email.cognito-idp.amazonaws.com" }
      Action    = ["ses:SendEmail", "ses:SendRawEmail"]
      Resource  = aws_sesv2_email_identity.main[0].arn
      Condition = {
        StringEquals = {
          "aws:SourceAccount" = data.aws_caller_identity.current.account_id
        }
        ArnLike = {
          "aws:SourceArn" = "arn:aws:cognito-idp:${var.aws_region}:${data.aws_caller_identity.current.account_id}:userpool/*"
        }
      }
    }]
  })
}
