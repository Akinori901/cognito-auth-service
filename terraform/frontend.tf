# -----------------------------------------------------------------------------
# 認証コンソールのフロント配信（S3 + CloudFront）
# -----------------------------------------------------------------------------
#
# task-scope と同じ構成にする。CloudFront が S3（画面）と API Gateway（API）を
# 1 つのドメインにまとめるので、フロントは同一オリジンで /api を叩ける。
# CORS の設定を持たずに済むのが利点。

resource "aws_s3_bucket" "console_front" {
  bucket = "auth-console-front-${data.aws_caller_identity.current.account_id}"

  tags = {
    Name = "auth-console-front"
  }
}

resource "aws_s3_bucket_public_access_block" "console_front" {
  bucket                  = aws_s3_bucket.console_front.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# CloudFront からだけ読ませる（バケットは非公開のまま）。
resource "aws_cloudfront_origin_access_control" "console_front" {
  name                              = "auth-console-front"
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

resource "aws_s3_bucket_policy" "console_front" {
  bucket = aws_s3_bucket.console_front.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "cloudfront.amazonaws.com" }
      Action    = "s3:GetObject"
      Resource  = "${aws_s3_bucket.console_front.arn}/*"
      Condition = {
        StringEquals = {
          "AWS:SourceArn" = aws_cloudfront_distribution.console.arn
        }
      }
    }]
  })
}

resource "aws_cloudfront_distribution" "console" {
  enabled             = true
  default_root_object = "index.html"
  comment             = "Auth Console"
  price_class         = "PriceClass_200" # 日本を含む。全世界配信は不要

  origin {
    origin_id                = "s3-frontend"
    domain_name              = aws_s3_bucket.console_front.bucket_regional_domain_name
    origin_access_control_id = aws_cloudfront_origin_access_control.console_front.id
  }

  origin {
    origin_id = "api-gateway"
    # HTTP API のエンドポイントからスキームを外してホスト名だけにする
    domain_name = replace(aws_apigatewayv2_api.api.api_endpoint, "https://", "")

    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = "https-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }

  # 画面（SPA）
  default_cache_behavior {
    target_origin_id       = "s3-frontend"
    viewer_protocol_policy = "redirect-to-https"
    allowed_methods        = ["GET", "HEAD", "OPTIONS"]
    cached_methods         = ["GET", "HEAD"]
    # CachingOptimized（AWS 管理ポリシー）
    cache_policy_id = "658327ea-f89d-4fab-a63d-7e88639e58f6"
  }

  # API は絶対にキャッシュしない。
  # 認可の判定結果をキャッシュすると、権限を剥奪しても通り続ける。
  ordered_cache_behavior {
    path_pattern           = "/api/*"
    target_origin_id       = "api-gateway"
    viewer_protocol_policy = "redirect-to-https"
    allowed_methods        = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
    cached_methods         = ["GET", "HEAD"]
    # CachingDisabled
    cache_policy_id = "4135ea2d-6df8-44a3-9df3-4b5a84be39ad"
    # AllViewerExceptHostHeader（Authorization ヘッダを通す）
    origin_request_policy_id = "b689b0a8-53d0-40ab-baf2-68738e2966ac"
  }

  # SPA なので、存在しないパスは index.html に返して
  # クライアント側のルーティングに任せる。
  custom_error_response {
    error_code         = 403
    response_code      = 200
    response_page_path = "/index.html"
  }

  custom_error_response {
    error_code         = 404
    response_code      = 200
    response_page_path = "/index.html"
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  viewer_certificate {
    cloudfront_default_certificate = true
  }

  tags = {
    Name = "auth-console"
  }
}
