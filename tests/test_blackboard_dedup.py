"""engine/blackboard.py 去重管线与合并语义（SPEC 5.3）。

全局 Blackboard 与 BlackboardPartition 共用模块级 `_find_similar_in` 五层
管线（registry #10），两类 `_find_similar_hypothesis` 均为委托。合并语义
（置信度 boost、[merged] 证据、DISCARDED 复活阈值 0.4）经 add_hypothesis
公开入口验证。分层用例 wording 留足阈值余量，避免停用词表边缘效应。
"""

from pathlib import Path

import pytest

from engine.blackboard import Blackboard, HypothesisStatus, classify_hypothesis_polarity


@pytest.fixture
def bb(tmp_path: Path) -> Blackboard:
    return Blackboard(work_dir=tmp_path)


# ───────────── 极性 ─────────────


class TestPolarity:
    def test_negative_hypothesis_rejected(self, bb):
        assert classify_hypothesis_polarity("memcpy is not vulnerable to overflow") == "negative"

    def test_positive_by_default(self, bb):
        assert classify_hypothesis_polarity("") == "positive"
        assert classify_hypothesis_polarity("buffer overflow in parse_header") == "positive"

    async def test_add_negative_returns_none(self, bb):
        h_id = await bb.add_hypothesis("This is not exploitable and poses no risk", 0.7)
        assert h_id is None
        assert len(bb.hypotheses) == 0


# ───────────── Layer 0 锚点匹配 ─────────────


class TestLayer0Anchor:
    async def test_shared_function_anchor_with_moderate_overlap(self, bb):
        base = "TIFFReadDirEntryArray in tif_dirread.c allocates count typesize bytes without overflow check"
        # 变体必须自带锚点才会进 Layer 0（锚点交集 + unigram≥0.30 → 合并）；
        # 该对 bigram 0.09 / unigram 0.40，低层均不会命中，确为锚点层合并。
        variant = "tif_dirread.c allocates array without validating count"
        first = await bb.add_hypothesis(base, 0.6)
        second = await bb.add_hypothesis(variant, 0.7)
        assert first is not None
        assert second == first
        assert len(bb.hypotheses) == 1


# ───────────── Layer 1 结构指纹 ─────────────


class TestLayer1Fingerprint:
    async def test_identical_description_merges(self, bb):
        desc = "stack buffer overflow in parse_config when copying token into fixed buffer"
        first = await bb.add_hypothesis(desc, 0.5)
        second = await bb.add_hypothesis(desc, 0.6)
        assert second == first
        assert len(bb.hypotheses) == 1


# ───────────── 模糊去重（公开入口，registry #8 回归）─────────────


class TestFuzzyDedupViaPublicApi:
    async def test_fuzzy_dedup_works_via_public_api(self, bb):
        # 近似措辞对（bigram 0.75，无锚点、指纹不同——确为模糊层）经
        # add_hypothesis 公开入口合并为 1 节点；布隆预筛删除前会被误判为新节点。
        base = "heap overflow in parser when copying token into fixed buffer"
        variant = "heap overflow in parser when copying token into fixed array"
        first = await bb.add_hypothesis(base, 0.6)
        second = await bb.add_hypothesis(variant, 0.7)
        assert second == first
        assert len(bb.hypotheses) == 1


# ───────────── 合并语义 ─────────────


class TestMergeSemantics:
    async def test_confidence_boost_and_merged_evidence(self, bb):
        desc = "integer underflow in gxclread.c band read process leads to huge memcpy"
        first = await bb.add_hypothesis(desc, 0.5)
        await bb.add_hypothesis(desc, 0.6)
        node = bb.hypotheses[first]
        # 首次合并：已有 [merged] 证据数=0 → boost=0 → new = max(0.5, 0.6) = 0.6
        assert node.confidence == pytest.approx(0.6)
        assert any("[merged]" in e for e in node.evidence)

    async def test_distinct_hypotheses_not_merged(self, bb):
        a = await bb.add_hypothesis("sql injection in login form through username parameter", 0.6)
        b = await bb.add_hypothesis(
            "race condition in filesystem journal replay during crash recovery", 0.6
        )
        assert a is not None and b is not None and a != b
        assert len(bb.hypotheses) == 2

    async def test_discarded_hypothesis_revival(self, bb):
        # registry #9 回归：DISCARDED 不再被去重跳过；精确重复以 ≥0.4 置信度
        # 再提案 → 合并进同一节点并复活为 PENDING。
        desc = "use after free in connection pool cleanup path"
        first = await bb.add_hypothesis(desc, 0.5)
        await bb.update_hypothesis(first, status=HypothesisStatus.DISCARDED)
        second = await bb.add_hypothesis(desc, 0.4)
        assert second == first
        assert len(bb.hypotheses) == 1
        assert bb.hypotheses[first].status == HypothesisStatus.PENDING

    async def test_discarded_hypothesis_below_revival_threshold(self, bb):
        # registry #9 边界：复活阈值 0.4 以下再提案 → 仍合并（1 节点），
        # 但保持 DISCARDED。
        desc = "use after free in connection pool cleanup path"
        first = await bb.add_hypothesis(desc, 0.5)
        await bb.update_hypothesis(first, status=HypothesisStatus.DISCARDED)
        second = await bb.add_hypothesis(desc, 0.3)
        assert second == first
        assert len(bb.hypotheses) == 1
        assert bb.hypotheses[first].status == HypothesisStatus.DISCARDED


# ───────────── 分区统一管线（registry #10 回归）─────────────


class TestPartitionUnifiedPipeline:
    async def test_partition_anchor_layer_merges(self, bb):
        # 分区内共享函数锚点 parse_header + unigram 0.44 ≥ 0.30 的近似对经
        # 分区公开入口合并（bigram 0.10 / unigram 0.44：旧三层分区管线
        # bigram<0.55 且 unigram<0.65 本不合并，有效钉住漂移）。
        part = bb.create_partition("sector-parser")
        base = "parse_header allocates count typesize bytes without overflow check"
        variant = "parse_header allocates array without validating count"
        first = await part.add_hypothesis(base, 0.6)
        second = await part.add_hypothesis(variant, 0.7)
        assert second == first
        assert len(part.hypotheses) == 1
