#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common import ZabbixClone
from zc.replica.store import ReplicaStoreMixin
from zc.replica.global_settings import ReplicaGlobalSettingsMixin
from zc.replica.configuration import ReplicaConfigurationMixin
from zc.replica.api import ReplicaApiMixin
from zc.replica.host import ReplicaHostMixin
from zc.replica.alert import ReplicaAlertMixin
from zc.replica.authentication import ReplicaAuthenticationMixin
from zc.replica.checknow import ReplicaCheckNowMixin

class ZabbixReplica(
    ReplicaStoreMixin,
    ReplicaGlobalSettingsMixin,
    ReplicaConfigurationMixin,
    ReplicaApiMixin,
    ReplicaHostMixin,
    ReplicaAlertMixin,
    ReplicaAuthenticationMixin,
    ReplicaCheckNowMixin,
    ZabbixClone,
):
    '''
    Zabbix replica node operations class
    '''
    pass
