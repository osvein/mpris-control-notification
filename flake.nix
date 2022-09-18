{
  description = "Resident desktop notifications for controlling MPRIS media players";

  inputs.gomod2nix.url = "github:nix-community/gomod2nix";

  outputs = { self, nixpkgs, flake-utils, gomod2nix }: flake-utils.lib.eachDefaultSystem (system:
    let
      mpris-control-notification = {
        lib, buildGoApplication
      }: buildGoApplication rec {
        pname = "mpris-control-notification";
        version = "0.1";
        src = self;
        modules = ./gomod2nix.toml;
      };
      
      overlay = final: prev: with prev; {
        mpris-control-notification = callPackage mpris-control-notification {};
      };
      
      pkgs = import nixpkgs {
        inherit system;
        overlays = [ gomod2nix.overlays.default overlay ];
      };
    in {
      packages = {
        default = pkgs.mpris-control-notification;
      };
      devShells = {
        #default = pkgs.mpris-control-notification.overrideAttrs (prev: {
        #  nativeBuildInputs = prev.nativeBuildInputs ++ [ pkgs.gomod2nix ];
        #});
        default = pkgs.mkGoEnv {
          pwd = ./.;
          #nativeBuildInputs = [ pkgs.gomod2nix ];
        };
      };
    }
  );
}
