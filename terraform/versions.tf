terraform {
  required_version = ">= 1.5"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.0"
    }
  }

  # state はローカル管理（fvc / SkillLogger と同方式）。
  # 将来 S3 backend 化する場合はここに backend "s3" を追加する。
}

provider "aws" {
  region = var.aws_region

  # 誤アカウントへの apply を防ぐガード（SkillLogger と同方針）。
  allowed_account_ids = var.allowed_account_ids

  default_tags {
    tags = {
      Project     = "auth-user-pool"
      Environment = "production"
      ManagedBy   = "terraform"
    }
  }
}
