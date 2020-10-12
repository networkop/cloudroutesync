provider "google" {
  project = "example-291909"
  region  = "europe-west2"
  zone = "europe-west2-c"
  version = "=3.42.0"
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

sudo curl -L https://raw.githubusercontent.com/networkop/cloudroutesync/main/demo-docker/bgpd.conf -o /home/example/bgpd.conf
sudo curl -L https://raw.githubusercontent.com/networkop/cloudroutesync/main/demo-docker/daemons -o /home/example/daemons
sudo curl -L https://raw.githubusercontent.com/networkop/cloudroutesync/main/demo-docker/docker-compose.yml -o /home/example/docker-compose.yml

CUSTOM_DATA
}


resource "google_compute_instance" "router_vm" {
  name         = "${var.prefix}-router-vm"
  machine_type = "e2-micro"

  boot_disk {
    initialize_params {
      image = "ubuntu-os-cloud/ubuntu-2004-lts"
    }
  }

  can_ip_forward = true
  network_interface {
    network = "default"
    access_config {
    }
  }
  
  metadata = {
    ssh-keys = "example:${file(var.pub_ssh_key_path)}"
    startup-script = local.custom_data
  }

  service_account {
    scopes = ["compute-rw"]
  }
}


resource "google_compute_instance" "normal_vm" {
  name         = "${var.prefix}-normal-vm"
  machine_type = "e2-micro"

  boot_disk {
    initialize_params {
      image = "ubuntu-os-cloud/ubuntu-2004-lts"
    }
  }

  can_ip_forward = true
  network_interface {
    network = "default"
    access_config {
    }
  }

  metadata = {
    ssh-keys = "example:${file(var.pub_ssh_key_path)}"
    startup-script = local.custom_data
  }

}