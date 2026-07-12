#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common.constants import *
from zc.common.logging import __LOGGER__, __HANDLER__
from zc.common.utils import *
from zc.common.cli import inputParameters
from zc.common.config import ZabbixCloneConfig
from zc.common.parameters import ZabbixCloneParameter
from zc.common.datastore import ZabbixCloneDatastore
from zc.common.zabbix_data import ZabbixDataMixin

__all__ = [name for name in globals() if not name.startswith("_")]
__all__.extend(["__LOGGER__", "__HANDLER__", "ZabbixClone"])

def __getattr__(name):
    if name == "ZabbixClone":
        from zc.clone.main import ZabbixClone
        return ZabbixClone
    raise AttributeError(name)
