package updates

func templatePlaybook() {
	updatePlaybook := `
---
- name: Run the ostree update 
  hosts: localhost
  vars:
    ostree_remote_name: {{remote}}
    ostree_remote_url: 192.168.122.15:8000/
    ostree_content_url: 192.168.122.15:8000/repo
    ostree_gpg_verify: "false"
    ostree_remote_template: |
      [remote "{{ostree_remote_name}}"]
      url={{ostree_remote_url}}
      gpg-verify={{ostree_gpg_verify|default("true")}}
      gpgkeypath={{ostree_gpg_keypath|default("/etc/pki/rpm-gpg/")}}
      contenturl={{ostree_content_url}}
     
  tasks:
    - name: apply templated ostree remote config
      ansible.builtin.copy:
        content: "{{ostree_remote_template}}"
        dest: /etc/ostree/remotes.d/rhel-for-edge.conf

    - name: run rpmostree update
      ansible.builtin.shell: rpm-ostree upgrade
      register: rpmostree_upgrade_out
      changed_when: '"No upgrade available" not in rpmostree_upgrade_out.stdout'
      failed_when: 'rpmostree_upgrade_out.rc != 0'

`
}

func dispatchPlaybook() {

}
