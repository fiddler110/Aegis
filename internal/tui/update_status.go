package tui

import (
	tea "charm.land/bubbletea/v2"
)

// updateTeammatesUpdate applies the P2.5 silent sub-agent poll.
func (m model) updateTeammatesUpdate(msg teammatesUpdateMsg) (tea.Model, tea.Cmd) {
	m.teammates = msg.items
	m.refresh()
	return m, nil
}

// updateTeammates renders an explicitly requested teammate roster.
func (m model) updateTeammates(msg teammatesMsg) (tea.Model, tea.Cmd) {
	m.renderTeammates(msg)
	m.refresh()
	return m, nil
}

// updateStatusInfo folds a /status poll into the header's context and
// connection indicators.
func (m model) updateStatusInfo(msg statusInfoMsg) (tea.Model, tea.Cmd) {
	// Silent: a daemon that predates /status context fields (or an
	// unreachable one) just leaves the name-based fallback in place.
	if msg.err == nil && msg.info.ContextWindow > 0 {
		m.srvCtxWin = msg.info.ContextWindow
		m.srvCtxWinSrc = msg.info.ContextWindowSource
	}
	// P81.22/FIND-22: keep the last known backend on a transient /status
	// error rather than blanking it — a daemon hiccup shouldn't make the
	// "commands run unconfined" signal flicker off.
	if msg.err == nil && msg.info.SandboxBackend != "" {
		m.sandboxBackend = msg.info.SandboxBackend
	}
	// P81.23/FIND-23: same "keep the last known value on a transient error"
	// posture as SandboxBackend above.
	if msg.err == nil {
		m.cronJobCount = msg.info.CronJobCount
		m.cronAutoApproveCount = msg.info.CronAutoApproveCount
		m.cronUnconfirmedCount = msg.info.CronUnconfirmedCount
	}
	// P28.7 connection/model-health indicator: a request error means the
	// daemon itself is unreachable, distinct from the daemon being up but
	// reporting its configured provider as unreachable.
	m.connKnown = true
	if msg.err != nil {
		m.connReachable, m.connLatencyMS = false, 0
	} else {
		m.connReachable = msg.info.ProviderReachable
		m.connLatencyMS = msg.info.ProviderLatencyMS
	}
	return m, nil
}

// updateCronJobs applies a silent ListCronJobs poll result to the sidebar's
// live job listing (P<dashboard>), mirroring updateTeammatesUpdate above.
func (m model) updateCronJobs(msg cronJobsUpdateMsg) (tea.Model, tea.Cmd) {
	m.cronJobs = msg.items
	m.refresh()
	return m, nil
}

// updateStatusTick re-arms the periodic /status poll.
func (m model) updateStatusTick(msg statusTickMsg) (tea.Model, tea.Cmd) {
	return m, tea.Batch(m.fetchStatusInfo(), m.fetchCronJobsQuiet(), statusTickCmd())
}
