# application-contract

Ce dépôt définit et valide le contrat `application.yaml` des applications du homelab.

```bash
nix run github:sachahjkl/application-contract -- validate
```

Pour un volume Nomad utilisé par un processus non privilégié, définissez `volume.uid` et `volume.gid` ensemble. Le contrat transmet ces identifiants au plugin `mkdir` lors de la création du volume.
