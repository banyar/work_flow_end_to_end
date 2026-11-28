# What each 🟡/❌ AC still needs before it can be ✅ (shown in coverage_ui.html).
# kind: test = a unit test to write, code = a code change (then a test),
#       decide = a PO/team decision (the code differs from the AC text).
# where: the file the test or change goes in (relative to the RT folder).
NA = 'NocAutomationCodeMerge/noc_automation/frontiir/'
RU = 'NocAutomationCodeMerge/remote-resolved-queue-transfer/internal/'


def n(kind, mm, en, where=''):
    return dict(kind=kind, mm=mm, en=en, where=where)


NEEDS = {
}
