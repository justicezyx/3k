# Users

## Primary targets

- Consumer (SysUserConsumer, user_type = 2). The account that trains, fine-tunes, runs inference, and uses Jupyter. Balance and GPU quota are checked on this user id.
- Supplier (SysUserSupplier, user_type = 3). The GPU-cluster owner who brings a cluster onto the platform. Registration stores company name, phone, and a company id. After the cluster is onboarded, this account no longer administers it. Changes such as leaving the federation go through an ops channel to the super admin.

## Admin roles

- CPod admin (SysUserAdmin, admin = 1). An operator assigned to one CPod to manage that cluster. This is not the cluster owner. The role exists so cluster changes do not require the super admin.
- Super admin (SysUserSuperAdmin, admin = 2). The SXWL.AI team. Overall admin of the whole platform, including federation changes the CPod admin and the supplier cannot make.
