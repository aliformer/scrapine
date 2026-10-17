#!/bin/bash
# Run on Proxmox host to create VM
# Adjust: storage name, ISO path, network bridge

VMID=200
STORAGE="local-lvm"
ISO_PATH="local:iso/debian-12-genericcloud-amd64.qcow2"
BRIDGE="vmbr0"

qm create $VMID \
  --name gmaps-scraper \
  --memory 4096 \
  --cores 4 \
  --cpu cputype=host \
  --net0 virtio,bridge=$BRIDGE \
  --scsihw virtio-scsi-pci \
  --scsi0 ${STORAGE}:32 \
  --ide2 ${ISO_PATH},media=cdrom \
  --boot order=scsi0 \
  --ostype l26 \
  --agent enabled=1

qm start $VMID
echo "VM $VMID created and started. Get IP: qm guest cmd $VMID network get-interfaces"
