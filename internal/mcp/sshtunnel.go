package mcp

import (
	"context"
	"errors"
	"log/slog"

	"github.com/ThowiLabs/kaggle-mcp/internal/sshtunnel"
)

// attachSSHTunnel crea el gestor del túnel SSH inverso y, si existe una
// configuración guardada, la descifra con el PIN e inicia el túnel en
// segundo plano. Nunca falla el arranque: sin configuración, con PIN
// generado o con descifrado fallido, el servidor sigue con su URL
// temporal y solo registra una advertencia.
//
// El PIN en claro solo existe en este arranque: el gestor guarda la
// contraseña descifrada en memoria, nunca el PIN.
func (s *Server) attachSSHTunnel(ctx context.Context, pin, nonce string, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	mgr := sshtunnel.New(s.cfg.DataDir, s.cfg.MCPPort, log)
	mgr.SetNonce(nonce)
	mgr.SetContext(ctx)
	s.ssh = mgr
	configured, err := mgr.Load()
	if err != nil {
		log.Warn("túnel SSH inverso no disponible: configuración inválida", "error", err)
		return
	}
	if !configured {
		return
	}
	if pin == "" {
		mgr.SetPINConfigured(false)
		log.Warn("túnel SSH configurado pero el PIN fue generado en este arranque; configura un PIN propio y vuelve a introducir la contraseña para que la configuración persista")
		return
	}
	mgr.SetPINConfigured(true)
	if err := mgr.Unlock(pin); err != nil {
		log.Warn("túnel SSH inverso no iniciado", "error", err)
		return
	}
	if st := mgr.Status(); !st.Enabled {
		return
	}
	if err := mgr.Start(); err != nil {
		log.Warn("túnel SSH inverso no iniciado", "error", err)
		return
	}
	log.Info("túnel SSH inverso iniciándose en segundo plano", "host", mgr.Status().Host)
}

func (s *Server) sshStatus() sshtunnel.Status {
	if s.ssh == nil {
		return sshtunnel.Status{}
	}
	return s.ssh.Status()
}

// configureSSH guarda (o reemplaza) la configuración del túnel SSH. El
// PIN se verifica y solo se usa para cifrar la contraseña; no se guarda.
func (s *Server) configureSSH(pin, user, host string, sshPort, remotePort int, password string, enabled bool) (sshtunnel.Status, error) {
	if s.ssh == nil {
		return sshtunnel.Status{}, errors.New("túnel SSH no disponible")
	}
	if !s.pinState.Verify(pin) {
		return s.ssh.Status(), errors.New("PIN incorrecto")
	}
	return s.ssh.Configure(pin, user, host, sshPort, remotePort, password, enabled)
}

// testSSH comprueba las credenciales y el reenvío sin guardar nada.
func (s *Server) testSSH(ctx context.Context, user, host string, sshPort, remotePort int, password string) (int, error) {
	if s.ssh == nil {
		return 0, errors.New("túnel SSH no disponible")
	}
	return s.ssh.Test(ctx, user, host, sshPort, remotePort, password)
}

// startSSH habilita y arranca el túnel con la contraseña en memoria.
func (s *Server) startSSH() (sshtunnel.Status, error) {
	if s.ssh == nil {
		return sshtunnel.Status{}, errors.New("túnel SSH no disponible")
	}
	return s.ssh.Enable()
}

// stopSSH detiene el túnel y persiste el deshabilitado. No borra la
// configuración: se puede volver a habilitar o reemplazar.
func (s *Server) stopSSH() (sshtunnel.Status, error) {
	if s.ssh == nil {
		return sshtunnel.Status{}, errors.New("túnel SSH no disponible")
	}
	return s.ssh.Disable()
}
