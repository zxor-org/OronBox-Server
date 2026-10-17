
CREATE TRIGGER resource_comments_hidden_insert_message AFTER INSERT ON resource_comments FOR EACH ROW EXECUTE FUNCTION notify_comment_hidden();
CREATE TRIGGER collection_revisions_result_message AFTER UPDATE OF state ON resource_collection_revisions FOR EACH ROW EXECUTE FUNCTION notify_collection_review_result();
CREATE TRIGGER coin_ledger_adjustment_message AFTER INSERT ON coin_ledger FOR EACH ROW EXECUTE FUNCTION notify_coin_adjustment();
CREATE TRIGGER resource_coin_votes_invalidated_message AFTER UPDATE OF invalidated_at ON resource_coin_votes FOR EACH ROW EXECUTE FUNCTION notify_coin_vote_invalidated();
CREATE TRIGGER review_appeals_result_message AFTER UPDATE OF status ON review_appeals FOR EACH ROW EXECUTE FUNCTION notify_appeal_result();
