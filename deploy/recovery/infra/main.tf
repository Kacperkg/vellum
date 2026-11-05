terraform {
  required_version = ">= 1.8"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }
}

provider "aws" {
  region = "eu-west-2"
}

data "aws_caller_identity" "current" {}

variable "authority_image" {
  type = string
  validation {
    condition     = can(regex("@sha256:[0-9a-f]{64}$", var.authority_image))
    error_message = "Supply a reviewed ECR image by digest."
  }
}

variable "authority_configuration" {
  description = "Public settings only. See the example. Store the GitHub token in the specified Secrets Manager resource."
  type        = any
}

variable "github_oidc_provider_arn" {
  type = string
}

variable "roles_anywhere_profile_arn" {
  description = "Existing separately reviewed Roles Anywhere profile ARN."
  type        = string
}

variable "roles_anywhere_trust_anchor_arn" {
  type = string
}

variable "relay_certificate_common_name" {
  type = string
}

locals {
  account  = data.aws_caller_identity.current.account_id
  names    = toset(["dispatch", "host"])
  pin      = var.authority_configuration.production
  instance = "arn:aws:ec2:eu-west-2:${local.account}:instance/${local.pin.instance}"
  config = merge(var.authority_configuration, {
    account        = local.account
    table          = aws_dynamodb_table.invocations.name
    issuer_key_arn = aws_kms_key.issuer.arn
  })
  receipt_bucket = "arn:aws:s3:::${var.authority_configuration.receipt_bucket}"
}

resource "aws_kms_key" "issuer" {
  description              = "Vellum production readiness issuer only"
  customer_master_key_spec = "ECC_NIST_EDWARDS25519"
  key_usage                = "SIGN_VERIFY"
  deletion_window_in_days  = 30
}

resource "aws_dynamodb_table" "invocations" {
  name         = "vellum-production-invocations"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "key"
  attribute {
    name = "key"
    type = "S"
  }
  ttl {
    attribute_name = "expiry"
    enabled        = true
  }
  point_in_time_recovery {
    enabled = true
  }
  deletion_protection_enabled = true
}

resource "aws_cloudwatch_log_group" "authority" {
  for_each          = local.names
  name              = "/aws/lambda/vellum-production-${each.key}"
  retention_in_days = 30
}

resource "aws_iam_role" "authority" {
  for_each = local.names
  name     = "vellum-production-${each.key}"
  assume_role_policy = jsonencode({
    Version   = "2012-10-17"
    Statement = [{ Effect = "Allow", Action = "sts:AssumeRole", Principal = { Service = "lambda.amazonaws.com" } }]
  })
}

resource "aws_iam_role_policy" "authority" {
  for_each = local.names
  role     = aws_iam_role.authority[each.key].id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = concat([
      { Effect = "Allow", Action = ["logs:CreateLogStream", "logs:PutLogEvents"], Resource = "${aws_cloudwatch_log_group.authority[each.key].arn}:*" },
      { Effect = "Allow", Action = ["secretsmanager:GetSecretValue"], Resource = local.config.github_secret },
      { Effect = "Allow", Action = ["ssm:GetCommandInvocation", "ssm:ListCommands"], Resource = "*" },
      { Effect = "Allow", Action = ["ssm:DescribeDocument"], Resource = [local.pin.document, local.config.staging.document] },
      { Effect = "Allow", Action = ["s3:GetObject"], Resource = "${local.receipt_bucket}/production/recovery/*" },
      { Effect = "Allow", Action = ["s3:ListBucket"], Resource = local.receipt_bucket,
      Condition = { StringLike = { "s3:prefix" = "production/recovery/*" } } },
      { Effect = "Allow", Action = ["dynamodb:GetItem", "dynamodb:UpdateItem", "dynamodb:PutItem"], Resource = aws_dynamodb_table.invocations.arn }
      ], [for role in [each.key] :
      { Effect = "Allow", Action = ["ssm:SendCommand"], Resource = [local.pin.document, local.instance] }
      if role == "dispatch"
      ], [for role in [each.key] :
      { Effect = "Allow", Action = ["kms:Sign"], Resource = aws_kms_key.issuer.arn,
      Condition = { StringEquals = { "kms:SigningAlgorithm" = "ED25519_SHA_512" } } }
      if role == "host"
    ])
  })
}

resource "aws_lambda_function" "authority" {
  for_each      = local.names
  function_name = "vellum-production-${each.key}"
  role          = aws_iam_role.authority[each.key].arn
  package_type  = "Image"
  architectures = ["x86_64"]
  image_uri     = var.authority_image
  publish       = true
  timeout       = each.key == "dispatch" ? 150 : 22
  memory_size   = 256
  environment {
    variables = {
      VELLUM_AUTHORITY_ENDPOINT = each.key
      VELLUM_AUTHORITY_CONFIG   = jsonencode(local.config)
    }
  }
  depends_on = [aws_iam_role_policy.authority]
}

resource "aws_iam_role" "runner" {
  name = "vellum-production-automated-runner"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{ Effect = "Allow", Action = "sts:AssumeRoleWithWebIdentity",
      Principal = { Federated = var.github_oidc_provider_arn },
      Condition = { StringEquals = {
        "token.actions.githubusercontent.com:aud" = "sts.amazonaws.com"
        "token.actions.githubusercontent.com:sub" = "repo:Kacperkg/vellum:environment:production"
    } } }]
  })
}

resource "aws_iam_role_policy" "runner" {
  role = aws_iam_role.runner.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      { Effect = "Allow", Action = ["lambda:InvokeFunction"], Resource = aws_lambda_function.authority["dispatch"].qualified_arn }
    ]
  })
}

resource "aws_iam_role" "relay" {
  name = "vellum-production-automated-relay"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{ Effect = "Allow", Action = ["sts:AssumeRole", "sts:TagSession", "sts:SetSourceIdentity"],
      Principal = { Service = "rolesanywhere.amazonaws.com" },
      Condition = {
        ArnEquals    = { "aws:SourceArn" = var.roles_anywhere_trust_anchor_arn }
        StringEquals = { "aws:PrincipalTag/x509Subject/CN" = var.relay_certificate_common_name }
    } }]
  })
}

resource "aws_iam_role_policy" "relay" {
  role = aws_iam_role.relay.id
  policy = jsonencode({
    Version   = "2012-10-17"
    Statement = [{ Effect = "Allow", Action = ["lambda:InvokeFunction"], Resource = aws_lambda_function.authority["host"].qualified_arn }]
  })
}

output "pins" {
  value = {
    github_runner_role     = aws_iam_role.runner.arn
    github_dispatch        = aws_lambda_function.authority["dispatch"].qualified_arn
    host_relay_role        = aws_iam_role.relay.arn
    host_relay_function    = aws_lambda_function.authority["host"].qualified_arn
    roles_anywhere_profile = var.roles_anywhere_profile_arn
    issuer_key             = aws_kms_key.issuer.arn
  }
}
