package acceptance

// Ordinary and longest inputs exercise different allocation shapes. An actual
// fixed-candidate diagnostic showed a native ordinary-input RSS plateau distinct
// from the single-longest completion, plus supervisor RSS growth that later
// stopped at idle. The native HTTP pool and per-request vectors are possible
// contributors, not proof of a particular malloc mechanism.
//
// Only predeclared PRE-measurement calibration windows enter this function.
// Neither the 3x20 workload nor its post-repeat idle observations can refit it.
func calibrateWarmedResidentEnvelope(coldIdle, longest []residentResourcePoint, warmupCompletion residentResourcePoint, warmedIdle []residentResourcePoint) (residentEnvelope, error) {
	cold, err := calibrateResidentEnvelope(coldIdle, longest)
	if err != nil {
		return residentEnvelope{}, err
	}
	shapes := append(append([]residentResourcePoint(nil), longest...), warmupCompletion)
	warm, err := calibrateResidentEnvelope(warmedIdle, shapes)
	if err != nil {
		return residentEnvelope{}, err
	}
	high, spread := cold.Maximum.values(), cold.IdleSpread.values()
	warmHigh, warmSpread := warm.Maximum.values(), warm.IdleSpread.values()
	for i := range high {
		high[i] = max(high[i], warmHigh[i])
		spread[i] = max(spread[i], warmSpread[i])
		if spread[i] > ^uint64(0)-high[i] {
			return residentEnvelope{}, errResidentEnvelope
		}
	}
	return residentEnvelope{Maximum: residentBounds(high), IdleSpread: residentBounds(spread)}, nil
}
