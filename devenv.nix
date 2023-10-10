{ pkgs, ... }:

{
  languages.go.enable = true;
  languages.terraform.enable = true;
  languages.terraform.package = pkgs.opentofu;
  languages.python.enable = true;
}
