To generate session token for MFA accounts do:

0. Make sure AWS account is configured with an access key

```
aws configure
```

1. Get the new set of MFA credentials using the 6-digit token code

```
AWS_AUTH=$(aws sts get-session-token --serial-number arn:aws:iam::1234567890:mfa/root-account-mfa-device --token-code 123456)
```

2. Export the session token variable

```
export AWS_SESSION_TOKEN=$(echo $AWS_AUTH | jq -r '.Credentials.SessionToken')
```