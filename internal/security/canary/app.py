"""Canary: obvious injection / weak-crypto patterns for bandit and opengrep."""

import hashlib
import os
import subprocess

HARDCODED_PASSWORD = "correct-horse-battery-staple"


def ping(host):
    subprocess.call("ping -c 1 " + host, shell=True)


def calculate(expression):
    return eval(expression)


def digest(value):
    return hashlib.md5(value.encode()).hexdigest()


def report(host):
    os.system("logger " + host)
