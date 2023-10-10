terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 4.0"
    }
  }

  backend "s3" {
    bucket = "shopware-gh-import"
    key    = "tf"
    region = "eu-central-1"
  }
}

provider "aws" {
  region = "eu-central-1"
}

module "lambda_function" {
  source        = "terraform-aws-modules/lambda/aws"
  function_name = "gh-slack-import"
  handler       = "bootstrap"
  runtime       = "provided.al2"
  memory_size   = 128
  architectures = ["arm64"]
  timeout       = 60

  source_path = [{
    path = "lambda/slack"
    commands = [
      "CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bootstrap",
      ":zip",
    ],
    patterns = [
      "!.*",
      "bootstrap",
    ]
  }]

  environment_variables = merge(
    var.lambda_env,
    {
      "SQS_IMPORT_QUEUE" = aws_sqs_queue.gh-import-queue.url
    }
  )

  create_lambda_function_url = true
  attach_policy_statements   = true
  policy_statements = {
    sqs = {
      effect    = "Allow",
      actions   = ["sqs:SendMessage"],
      resources = [aws_sqs_queue.gh-import-queue.arn]
    }
  }
}

module "queue" {
  source        = "terraform-aws-modules/lambda/aws"
  function_name = "gh-slack-queue"
  handler       = "bootstrap"
  runtime       = "provided.al2"
  memory_size   = 1024
  architectures = ["arm64"]
  timeout       = 300

  source_path = [{
    path = "lambda/importer"
    commands = [
      "CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bootstrap",
      ":zip",
    ],
    patterns = [
      "!.*",
      "bootstrap",
    ]
  }]

  environment_variables = merge(
    var.lambda_env,
    {
      "SQS_IMPORT_QUEUE" = aws_sqs_queue.gh-import-queue.url
    }
  )

  layers = [
    "arn:aws:lambda:eu-central-1:094913474201:layer:shyim-git-arm64:3"
  ]

  attach_policy_statements = true
  policy_statements = {
    sqs = {
      effect    = "Allow",
      actions   = ["sqs:ReceiveMessage", "sqs:DeleteMessage", "sqs:GetQueueAttributes"],
      resources = [aws_sqs_queue.gh-import-queue.arn]
    }
  }

  event_source_mapping = {
    sqs = {
      event_source_arn        = aws_sqs_queue.gh-import-queue.arn
      function_response_types = ["ReportBatchItemFailures"]
      scaling_config = {
        maximum_concurrency = 2
      }
    }
  }
}

resource "aws_sqs_queue" "gh-import-queue" {
  name                       = "gh-import-queue"
  visibility_timeout_seconds = 300
}

output "url" {
  value = module.lambda_function.lambda_function_url
}