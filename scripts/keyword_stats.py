"""booksource-tool 关键词统计脚本

用法: python3 scripts/keyword_stats.py [mongo_uri] [mongo_db]

默认连接本地 127.0.0.1:27018 / booksource_tool
远程用: python3 scripts/keyword_stats.py mongodb://host:port/db booksource_tool
或: ssh tx 'python3 -c "$(cat scripts/keyword_stats.py)"'
"""

import sys, pymongo
from pymongo import MongoClient

MONGO_URI = sys.argv[1] if len(sys.argv) > 1 else "mongodb://127.0.0.1:27018"
MONGO_DB = sys.argv[2] if len(sys.argv) > 2 else "booksource_tool"

client = MongoClient(MONGO_URI)
db = client[MONGO_DB]

total = db.dedup_sources.count_documents({})
probed = db.dedup_sources.count_documents({"lastProbe.sampleTotal": {"$gt": 0}})
print("=== 进度 ===")
print("总源数:", total)
print("已探测:", probed)

# 关键词探测总量
pipe = [
    {"$match": {"lastProbe.sampleTotal": {"$gt": 0}}},
    {"$unwind": {"path": "$lastProbe.sampleResults", "preserveNullAndEmptyArrays": False}},
    {"$group": {"_id": None,
        "totalProbes": {"$sum": 1},
        "totalOk": {"$sum": {"$cond": ["$lastProbe.sampleResults.ok", 1, 0]}},
        "totalFail": {"$sum": {"$cond": ["$lastProbe.sampleResults.ok", 0, 1]}},
        "distinctKws": {"$addToSet": "$lastProbe.sampleResults.keyword"}
    }}
]
r = list(db.dedup_sources.aggregate(pipe))
if r:
    d = r[0]
    total_p = d["totalProbes"]
    ok_p = d["totalOk"]
    fail_p = d["totalFail"]
    print("\n=== 关键词探测总量 ===")
    print("总探测次数:", total_p)
    print("成功:", ok_p, "(%.1f%%)" % (ok_p/total_p*100))
    print("失败:", fail_p, "(%.1f%%)" % (fail_p/total_p*100))
    print("去重关键词数:", len(d["distinctKws"]))

# 失败关键词 Top
pipe2 = [
    {"$match": {"lastProbe.sampleTotal": {"$gt": 0}}},
    {"$unwind": "$lastProbe.sampleResults"},
    {"$match": {"lastProbe.sampleResults.ok": False}},
    {"$group": {"_id": "$lastProbe.sampleResults.keyword", "cnt": {"$sum": 1}}},
    {"$sort": {"cnt": -1}},
    {"$limit": 15}
]
fail_kws = list(db.dedup_sources.aggregate(pipe2))
print("\n=== 失败关键词 Top 15 ===")
for k in fail_kws:
    print("  %s: %d" % (k["_id"], k["cnt"]))

# 成功关键词 Top
pipe3 = [
    {"$match": {"lastProbe.sampleTotal": {"$gt": 0}}},
    {"$unwind": "$lastProbe.sampleResults"},
    {"$match": {"lastProbe.sampleResults.ok": True}},
    {"$group": {"_id": "$lastProbe.sampleResults.keyword", "cnt": {"$sum": 1}}},
    {"$sort": {"cnt": -1}},
    {"$limit": 15}
]
ok_kws = list(db.dedup_sources.aggregate(pipe3))
print("\n=== 成功关键词 Top 15 ===")
for k in ok_kws:
    print("  %s: %d" % (k["_id"], k["cnt"]))

# 关键词覆盖度
pipe4 = [
    {"$match": {"lastProbe.sampleTotal": {"$gt": 0}}},
    {"$group": {"_id": None,
        "avgKwPerSource": {"$avg": {"$size": {"$ifNull": ["$lastProbe.sampleKeywords", []]}}},
        "maxKw": {"$max": {"$size": {"$ifNull": ["$lastProbe.sampleKeywords", []]}}}
    }}
]
cov = list(db.dedup_sources.aggregate(pipe4))
if cov:
    c = cov[0]
    print("\n=== 关键词覆盖度 ===")
    print("平均每源关键词数:", "%.1f" % c["avgKwPerSource"])
    print("单源最大关键词数:", c["maxKw"])

# sampleTotal 分布
pipe5 = [
    {"$match": {"lastProbe": {"$exists": True}}},
    {"$group": {"_id": "$lastProbe.sampleTotal", "count": {"$sum": 1}}},
    {"$sort": {"_id": 1}}
]
dist = list(db.dedup_sources.aggregate(pipe5))
print("\n=== sampleTotal 分布 ===")
for d in dist:
    print("  %s: %d 个源" % (d["_id"], d["count"]))

# detail/verify stats
detail_ok = db.dedup_sources.count_documents({"lastProbe.detailOk": True})
detail_total = db.dedup_sources.count_documents({"lastProbe.detailOk": {"$exists": True}})
content_ok = db.dedup_sources.count_documents({"lastProbe.contentOk": True})
print("\n=== 详情/正文验证 ===")
print("有 detailOk 记录:", detail_total)
print("  detailOk=true:", detail_ok)
print("  contentOk=true:", content_ok)

client.close()
