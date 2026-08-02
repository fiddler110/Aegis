// Canary: obvious injection sinks for njsscan and opengrep.
const express = require('express');
const { exec } = require('child_process');

const app = express();

app.get('/ping', (req, res) => {
  exec('ping -c 1 ' + req.query.host, (err, stdout) => res.send(stdout));
});

app.get('/run', (req, res) => {
  res.send(eval(req.query.expression));
});

app.get('/echo', (req, res) => {
  res.send('<h1>' + req.query.name + '</h1>');
});

module.exports = app;
