#!/usr/bin/env python3
# -*- coding: utf-8 -*-
from zc.common import *
from zc.replica.configuration_data import ReplicaConfigurationDataMixin
from zc.replica.configuration_template import ReplicaConfigurationTemplateMixin
from zc.replica.configuration_apply import ReplicaConfigurationApplyMixin

class ReplicaConfigurationMixin(
    ReplicaConfigurationDataMixin,
    ReplicaConfigurationTemplateMixin,
    ReplicaConfigurationApplyMixin,
):
    def setConfigurationToZabbix(self):
        '''
        STOREからZabbixインポートデータの生成、適用
        CONFIG_IMPORTセクション
        '''
        importData, templates, triggers = self.buildConfigurationImportData()
        valueMap = self.extractConfigurationValueMap(importData)
        templateTotal = len(templates)

        result = self.appendTemplateImportData(importData, templates, triggers, valueMap)
        if not result[0]:
            return result

        result = self.convertTemplateGroups(importData)
        if not result[0]:
            return result

        result = self.applyConfigurationImportData(importData, templateTotal)
        if not result[0]:
            return result

        # テンプレート適用したのでZabbixからデータを取得、IDREPLACEの更新
        result = self.getDataFromZabbix()
        if not result[0]:
            return result

        return ZC_COMPLETE
