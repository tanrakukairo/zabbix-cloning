#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common.parameters import ZabbixCloneParameter
from zc.common.datastore import ZabbixCloneDatastore
from zc.common.zabbix_data import ZabbixDataMixin
from zc.clone.lifecycle import CloneLifecycleMixin
from zc.clone.connection import CloneConnectionMixin
from zc.clone.version import CloneVersionMixin
from zc.clone.processing import CloneProcessingMixin

class ZabbixClone(
    CloneLifecycleMixin,
    CloneConnectionMixin,
    CloneVersionMixin,
    CloneProcessingMixin,
    ZabbixDataMixin,
    ZabbixCloneParameter,
    ZabbixCloneDatastore,
):
    '''
    Zabbix clone operation class.
    '''
    pass
