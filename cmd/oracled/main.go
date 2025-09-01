package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"cosmossdk.io/log"
	"github.com/GPTx-global/guru-v2/oralce/daemon"
	"github.com/rs/zerolog"
)

const (
	// 기본 설정 경로
	defaultConfigDir  = ".oracled"
	defaultConfigFile = "config.toml"

	// 재시작 관련 설정
	initialRestartDelay = 5 * time.Second
	maxRestartDelay     = 5 * time.Minute
	maxConsecutiveFails = 10

	// 헬스체크 설정
	healthCheckInterval = 30 * time.Second
	healthTimeout       = 10 * time.Second
)

// OracleDaemonRunner는 Oracle Daemon의 실행 및 관리를 담당
type OracleDaemonRunner struct {
	logger          log.Logger
	configPath      string
	shutdownSignals chan os.Signal

	// 재시작 관리
	consecutiveFails int
	lastFailureTime  time.Time
	restartDelay     time.Duration
}

// NewOracleDaemonRunner는 새로운 daemon runner를 생성
func NewOracleDaemonRunner() *OracleDaemonRunner {
	// 홈 디렉토리 기반 설정 경로 설정
	homeDir, err := os.UserHomeDir()
	if err != nil {
		panic(fmt.Sprintf("failed to get user home directory: %v", err))
	}

	configPath := filepath.Join(homeDir, defaultConfigDir, defaultConfigFile)

	// 로거 설정
	logger := log.NewLogger(os.Stdout, log.LevelOption(zerolog.InfoLevel))

	// 시그널 처리를 위한 채널 설정
	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(shutdownSignals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	return &OracleDaemonRunner{
		logger:          logger,
		configPath:      configPath,
		shutdownSignals: shutdownSignals,
		restartDelay:    initialRestartDelay,
	}
}

// Run은 Oracle daemon을 실행하고 관리
func (r *OracleDaemonRunner) Run() error {
	r.logger.Info("starting Oracle daemon runner")

	rootCtx := context.Background()

	for {
		// 컨텍스트 생성
		ctx, cancel := context.WithCancel(rootCtx)

		// Daemon 실행
		err := r.runDaemon(ctx, cancel)

		// 정상 종료인 경우
		if err == nil {
			r.logger.Info("Oracle daemon stopped gracefully")
			return nil
		}

		// 에러 로깅 및 재시작 로직
		r.logger.Error("Oracle daemon failed", "error", err)
		r.handleFailure()

		// 최대 연속 실패 횟수 체크
		if r.consecutiveFails >= maxConsecutiveFails {
			r.logger.Error("maximum consecutive failures reached, stopping",
				"failures", r.consecutiveFails)
			return fmt.Errorf("daemon failed %d consecutive times", r.consecutiveFails)
		}

		// 재시작 대기
		r.logger.Info("restarting daemon",
			"delay", r.restartDelay,
			"consecutive_failures", r.consecutiveFails)

		select {
		case <-time.After(r.restartDelay):
			// 재시작 진행
		case sig := <-r.shutdownSignals:
			r.logger.Info("shutdown signal received during restart delay", "signal", sig)
			return nil
		}

		// 메모리 정리
		runtime.GC()
	}
}

// runDaemon은 단일 daemon 인스턴스를 실행
func (r *OracleDaemonRunner) runDaemon(ctx context.Context, cancel context.CancelFunc) error {
	defer cancel()

	r.logger.Info("initializing Oracle daemon", "config_path", r.configPath)

	// Daemon 생성
	daemon, err := daemon.NewOracleDaemon(ctx, r.configPath)
	if err != nil {
		return fmt.Errorf("failed to create daemon: %w", err)
	}
	defer daemon.Stop()

	// Daemon 시작
	if err := daemon.Start(ctx); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	r.logger.Info("Oracle daemon started successfully")
	r.resetFailureCounter()

	// 헬스체크 고루틴 시작
	healthCtx, healthCancel := context.WithCancel(ctx)
	defer healthCancel()

	go r.runHealthCheck(healthCtx, daemon)

	// 메인 이벤트 루프
	for {
		select {
		case sig := <-r.shutdownSignals:
			r.logger.Info("shutdown signal received", "signal", sig)

			// SIGHUP의 경우 재시작을 위해 에러 반환
			if sig == syscall.SIGHUP {
				r.logger.Info("reloading daemon due to SIGHUP")
				return fmt.Errorf("reload requested")
			}

			// 다른 시그널은 정상 종료
			return nil

		case fatalErr := <-daemon.Fatal():
			return fmt.Errorf("daemon fatal error: %w", fatalErr)

		case <-ctx.Done():
			r.logger.Info("context cancelled, stopping daemon")
			return nil
		}
	}
}

// runHealthCheck는 주기적으로 daemon의 상태를 체크
func (r *OracleDaemonRunner) runHealthCheck(ctx context.Context, daemon *daemon.OracleDaemon) {
	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.performHealthCheck(daemon)
		}
	}
}

// performHealthCheck는 실제 헬스체크를 수행
func (r *OracleDaemonRunner) performHealthCheck(daemon *daemon.OracleDaemon) {
	// daemon이 실행 중인지 확인
	if !daemon.IsRunning() {
		r.logger.Error("health check failed: daemon is not running")
		return
	}

	// 메트릭을 통한 상태 확인
	metrics := daemon.GetMetrics()

	if metrics.HealthStatus != "healthy" {
		r.logger.Warn("daemon health status degraded",
			"status", metrics.HealthStatus,
			"uptime", metrics.Uptime,
			"errors", metrics.TotalErrors,
			"fatal_errors", metrics.FatalErrors)
	} else {
		r.logger.Debug("daemon health check passed",
			"uptime", metrics.Uptime,
			"events_processed", metrics.EventsProcessed,
			"jobs_completed", metrics.JobsCompleted,
			"tx_submitted", metrics.TxSubmitted)
	}
}

// handleFailure는 실패 상황을 처리하고 재시작 로직을 조정
func (r *OracleDaemonRunner) handleFailure() {
	now := time.Now()

	// 연속 실패 카운터 업데이트
	if r.lastFailureTime.IsZero() || now.Sub(r.lastFailureTime) < time.Minute {
		r.consecutiveFails++
	} else {
		// 마지막 실패로부터 1분 이상 지났으면 카운터 리셋
		r.consecutiveFails = 1
	}

	r.lastFailureTime = now

	// 백오프 전략: 연속 실패에 따라 재시작 지연시간 증가
	r.restartDelay = time.Duration(r.consecutiveFails) * initialRestartDelay
	if r.restartDelay > maxRestartDelay {
		r.restartDelay = maxRestartDelay
	}
}

// resetFailureCounter는 성공적인 시작 후 실패 카운터를 리셋
func (r *OracleDaemonRunner) resetFailureCounter() {
	r.consecutiveFails = 0
	r.restartDelay = initialRestartDelay
	r.lastFailureTime = time.Time{}
}

// setupConfigDirectory는 설정 디렉토리가 존재하는지 확인하고 생성
func setupConfigDirectory() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, defaultConfigDir)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	return nil
}

func main() {
	// 설정 디렉토리 설정
	if err := setupConfigDirectory(); err != nil {
		panic(fmt.Sprintf("failed to setup config directory: %v", err))
	}

	// Daemon runner 생성 및 실행
	runner := NewOracleDaemonRunner()

	// 실행
	if err := runner.Run(); err != nil {
		runner.logger.Error("Oracle daemon runner failed", "error", err)
		os.Exit(1)
	}

	runner.logger.Info("Oracle daemon runner stopped successfully")
}
