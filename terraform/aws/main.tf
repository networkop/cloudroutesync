provider "aws" {
  version = "=3.10.0"
  region = "eu-west-1"
}

locals {
  custom_data = <<CUSTOM_DATA
#!/bin/bash
wget https://golang.org/dl/go1.15.2.linux-amd64.tar.gz
sudo tar -C /usr/local -xzf go1.15.2.linux-amd64.tar.gz
echo 'PATH=$PATH:/usr/local/go/bin' >> /home/example/.profile

sudo apt-get update
sudo apt-get install -y \
    apt-transport-https \
    ca-certificates \
    curl \
    gnupg-agent \
    software-properties-common
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add -
sudo add-apt-repository \
  "deb [arch=amd64] https://download.docker.com/linux/ubuntu \
  $(lsb_release -cs) \
  stable"
sudo apt-get update
sudo apt-get install -y docker-ce docker-ce-cli containerd.io
sudo systemctl restart docker

sudo docker pull frrouting/frr:v7.4.0
sudo docker pull networkop/cloudroutesync
sudo curl -L "https://github.com/docker/compose/releases/download/1.27.4/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
sudo chmod +x /usr/local/bin/docker-compose

sudo curl -L https://raw.githubusercontent.com/networkop/cloudroutesync/main/demo-docker/bgpd.conf?token=AC7G2E5IQ4Y4YMTD7RB5ASC7QG3Y6 -o /home/example/bgpd.conf
sudo curl -L https://raw.githubusercontent.com/networkop/cloudroutesync/main/demo-docker/daemons?token=AC7G2E6ZA422TY2CEJUDFC27QG3YY -o /home/example/daemons
sudo curl -L https://raw.githubusercontent.com/networkop/cloudroutesync/main/demo-docker/docker-compose.yml?token=AC7G2EYTD3BE4ECL2TWYK6C7QG3ZG -o /home/example/docker-compose.yml

CUSTOM_DATA
}

resource "aws_vpc" "example" {
  cidr_block = "10.0.0.0/16"
}

resource "aws_internet_gateway" "default" {
  vpc_id = aws_vpc.example.id
}

resource "aws_route" "internet_access" {
  route_table_id         = aws_vpc.example.main_route_table_id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = aws_internet_gateway.default.id
}

resource "aws_subnet" "example" {
  vpc_id                  = aws_vpc.example.id
  cidr_block              = "10.0.1.0/24"
}


resource "aws_security_group" "example" {
  name        = "${var.prefix}-sg"
  vpc_id      = aws_vpc.example.id

  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

}

resource "aws_key_pair" "auth" {
  key_name   = "${var.prefix}-sshkey"
  public_key = file(var.pub_ssh_key_path)
}

data "aws_iam_policy_document" "assume" {
  statement {
    effect = "Allow"

    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

resource "aws_iam_policy" "router_policy" {
  name = "${var.prefix}-router-role"

  policy = <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
          "ec2:AssociateRouteTable",
          "ec2:DisassociateRouteTable",
          "ec2:CreateRoute",
          "ec2:DeleteRoute",
          "ec2:CreateRouteTable",
          "ec2:DeleteRouteTable",
          "ec2:DescribeRouteTables",
          "ec2:DescribeInstances",
          "ec2:DescribeNetworkInterfaces"
      ],
      "Resource": "*"
    }
  ]
}
EOF
}

resource "aws_iam_role" "assume_role" {
  name               = "${var.prefix}-role"
  assume_role_policy = data.aws_iam_policy_document.assume.json
}

resource "aws_iam_policy_attachment" "example" {
  name       = "${var.prefix}-policy-attachement"
  roles      = [aws_iam_role.assume_role.name]
  policy_arn = aws_iam_policy.router_policy.arn
}

resource "aws_iam_instance_profile" "routetable-profile" {
  name = "${var.prefix}-instance-profile"
  role = aws_iam_role.assume_role.name
}

resource "aws_instance" "router-vm" {
  instance_type = "t2.micro"
  ami = var.ubuntu_ami
  
  key_name = aws_key_pair.auth.id

  associate_public_ip_address = true
  source_dest_check = false 
  vpc_security_group_ids = [aws_security_group.example.id]
  subnet_id = aws_subnet.example.id

  user_data = local.custom_data

  iam_instance_profile        = aws_iam_instance_profile.routetable-profile.name
}

