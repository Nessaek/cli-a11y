'use strict';

const { collect } = require('./probes');
const vision = require('./checks/vision');
const motor = require('./checks/motor');
const clarity = require('./checks/clarity');
const report = require('./report');

/**
 * Run the whole audit: execute the target under every probe, then grade the
 * captured output. Checks never spawn anything themselves, so adding a rule
 * costs nothing at runtime.
 */
async function audit(target, opts = {}) {
  const started = Date.now();
  const probes = await collect(target, opts);
  const findings = [
    ...vision.checks(probes),
    ...motor.checks(probes),
    ...clarity.checks(probes),
  ];
  return {
    target,
    findings,
    probes,
    meta: {
      ...probes._meta,
      probeCount: Object.keys(probes).length - 1,
      durationMs: Date.now() - started,
    },
  };
}

module.exports = { audit, report };
