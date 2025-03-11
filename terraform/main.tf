terraform {
  required_providers {
    digitalocean = {
      source  = "digitalocean/digitalocean"
      version = "~> 2.0"
    }
  }
}

provider "digitalocean" {
  token = var.do_token
}

# Use existing SSH key directly with the fingerprint/id
resource "digitalocean_droplet" "holochain_node" {
  name     = var.node_name
  size     = var.droplet_size
  image    = "ubuntu-22-04-x64"
  region   = var.region
  ssh_keys = [var.ssh_key_id]

  # Copy files and run setup using local-exec
  provisioner "local-exec" {
    command = <<-EOT
      # Wait for SSH to be ready
      until ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null root@${self.ipv4_address} 'exit' 2>/dev/null
      do
        echo "Waiting for SSH..."
        sleep 5
      done

      # Copy files
      rsync -av -e "ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null" \
        ${path.module}/../scripts/ \
        root@${self.ipv4_address}:/tmp/scripts/
      rsync -av -e "ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null" \
        ${path.module}/../services/ \
        root@${self.ipv4_address}:/tmp/services/

      # Setup environment and run scripts
      ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null root@${self.ipv4_address} '
        echo "export HOLOCHAIN_VERSION=${var.holochain_version}" >> /root/.profile
        echo "export LAIR_PASSWORD=${var.lair_password}" >> /root/.profile
        echo "export HOLOCHAIN_PASSWORD=${var.holochain_password}" >> /root/.profile
        chmod +x /tmp/scripts/*.sh
        /tmp/scripts/setup.sh
        /tmp/scripts/test.sh
      '
    EOT
  }
}

output "droplet_ip" {
  value = digitalocean_droplet.holochain_node.ipv4_address
}

output "ssh_command" {
  value = "ssh root@${digitalocean_droplet.holochain_node.ipv4_address}"
}